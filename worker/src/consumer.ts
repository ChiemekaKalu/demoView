// Queue consumers for parse jobs and their dead letters. See the spec's
// "Queue consumer" section.

import type { ParseJob } from "./api";
import { isMatchId } from "./keys";
import { errorStack, errorText } from "./log";
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
      const id = message.body?.match_id;
      console.error(
        JSON.stringify({
          event: "consumer_error",
          match_id: id ?? null,
          attempt: message.attempts,
          error: errorText(err),
          stack: errorStack(err),
        }),
      );
      // Record why, so a dead letter doesn't end as "unknown error".
      if (typeof id === "string" && isMatchId(id)) {
        await setStatus(env.DB, id, "queued", {
          error: errorText(err),
          from: ["queued", "parsing"],
          log: { attempt: message.attempts },
        }).catch(() => {});
      }
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

  const attempt = message.attempts;
  await setStatus(env.DB, id, "parsing", { error: null, log: { attempt } });
  const started = Date.now();
  const result = await parser.parse(
    { match_id: id, demo_key: match.demo_key },
    { attempt, requestId: message.id },
  );
  const log = result.kind === "ok" ? console.log : console.warn;
  log(
    JSON.stringify({
      event: "parse_call",
      match_id: id,
      attempt,
      max_attempts: MAX_ATTEMPTS,
      final: attempt >= MAX_ATTEMPTS,
      duration_ms: Date.now() - started,
      status: result.status ?? null,
      result: result.kind,
      ...(result.kind !== "ok" ? { error: result.error } : {}),
    }),
  );

  switch (result.kind) {
    case "ok": {
      let file: MatchFile;
      try {
        file = asMatchFile(result.body);
      } catch (err) {
        // Retrying would get the same output back, so this is final.
        await setStatus(env.DB, id, "failed", {
          error: `bad parser output: ${errorText(err)}`,
          log: { attempt },
        });
        message.ack();
        return;
      }
      await saveParsedMatch(env.DB, id, file, Date.now(), { attempt });
      message.ack();
      return;
    }
    case "rejected":
      await setStatus(env.DB, id, "failed", { error: result.error, log: { attempt } });
      message.ack();
      return;
    case "transient":
      // Back in the queue until the retry. After the last attempt the queue
      // moves the message to the dead letter queue instead.
      await setStatus(env.DB, id, "queued", { error: result.error, log: { attempt } });
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
    console.error(
      JSON.stringify({
        event: "dead_letter",
        match_id: id ?? null,
        attempts: message.attempts,
        status: match?.status ?? null,
        last_error: match?.error ?? null,
      }),
    );
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
