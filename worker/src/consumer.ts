// Queue consumers for parse jobs and their dead letters. See the spec's
// "Queue consumer" section.

import type { ParseJob } from "./api";
import { isMatchId } from "./keys";
import { asMatchFile, type MatchFile } from "./match-file";
import { getMatch, saveParsedMatch, setStatus } from "./matches";
import type { Parser } from "./parser";

// Must match max_retries + 1 in wrangler.jsonc.
export const MAX_ATTEMPTS = 3;

export async function handleParseBatch(
  batch: MessageBatch<ParseJob>,
  env: Env,
  parser: Parser,
): Promise<void> {
  for (const message of batch.messages) {
    try {
      await handleParseJob(message, env, parser);
    } catch (err) {
      // Unexpected, e.g. D1 unavailable. Retry like any transient failure.
      console.error(
        JSON.stringify({ event: "consumer_error", match_id: message.body?.match_id, error: String(err) }),
      );
      message.retry();
    }
  }
}

async function handleParseJob(message: Message<ParseJob>, env: Env, parser: Parser): Promise<void> {
  const id = message.body?.match_id;
  const match = typeof id === "string" && isMatchId(id) ? await getMatch(env.DB, id) : null;
  // Only queued or parsing (a redelivery) matches need work. Anything else is
  // a stale or duplicate message.
  if (!id || !match || (match.status !== "queued" && match.status !== "parsing")) {
    console.log(
      JSON.stringify({ event: "job_skipped", match_id: id ?? null, status: match?.status ?? null }),
    );
    message.ack();
    return;
  }

  await setStatus(env.DB, id, "parsing", { error: null });
  const result = await parser.parse({ match_id: id, demo_key: match.demo_key });

  switch (result.kind) {
    case "ok": {
      let file: MatchFile;
      try {
        file = asMatchFile(result.body);
      } catch (err) {
        // Retrying would get the same output back, so this is final.
        await setStatus(env.DB, id, "failed", { error: `bad parser output: ${(err as Error).message}` });
        message.ack();
        return;
      }
      await saveParsedMatch(env.DB, id, file, Date.now());
      message.ack();
      return;
    }
    case "rejected":
      await setStatus(env.DB, id, "failed", { error: result.error });
      message.ack();
      return;
    case "transient":
      // Back in the queue until the retry. After the last attempt the queue
      // moves the message to the dead letter queue instead.
      await setStatus(env.DB, id, "queued", { error: result.error });
      message.retry();
      return;
  }
}

// Messages land here once every attempt has failed. Mark the match failed,
// keeping the last error the consumer recorded.
export async function handleDeadLetters(batch: MessageBatch<ParseJob>, env: Env): Promise<void> {
  for (const message of batch.messages) {
    const id = message.body?.match_id;
    const match = typeof id === "string" && isMatchId(id) ? await getMatch(env.DB, id) : null;
    if (match) {
      const reason = match.error ?? "unknown error";
      await setStatus(env.DB, match.id, "failed", {
        error: `parse failed after ${MAX_ATTEMPTS} attempts: ${reason}`,
        from: ["queued", "parsing"],
      });
    }
    message.ack();
  }
}
