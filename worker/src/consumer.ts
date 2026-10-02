// Queue consumers for parse jobs and their dead letters. See the spec's
// "Queue consumer" section.

import type { ParseJob } from "./api";
import { addEvent } from "./events";
import { isMatchId, newParseId } from "./keys";
import { errorStack, errorText } from "./log";
import { asMatchFile, type MatchFile } from "./match-file";
import {
  failure,
  getMatch,
  recordAttempt,
  saveParsedMatch,
  setReparseError,
  setStatus,
} from "./matches";
import type { Parser, ParseResult } from "./parser";
import { addContainerTime } from "./usage";

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
  const reparse = message.body?.reparse === true;
  const match = typeof id === "string" && isMatchId(id) ? await getMatch(env.DB, id) : null;
  // A parse needs a queued match, or a parsing one on redelivery. A reparse
  // needs a ready one. Anything else is a stale or duplicate message.
  const wanted = reparse ? ["ready"] : ["queued", "parsing"];
  if (!id || !match || !wanted.includes(match.status)) {
    console.log(
      JSON.stringify({ event: "job_skipped", match_id: id ?? null, reparse, status: match?.status ?? null }),
    );
    message.ack();
    return;
  }

  const attempt = message.attempts;
  const build = env.PARSER_BUILD;
  // A new prefix for every attempt, so no two attempts write the same files.
  const parse = { id: newParseId(), from: match.live_parse_id };
  const log = { attempt, ...(reparse ? { reparse } : {}) };
  await recordAttempt(env.DB, id, build);
  // A reparse leaves the match ready on its live parse until the new one
  // replaces it.
  if (!reparse) await setStatus(env.DB, id, "parsing", { error: null, log });
  const started = Date.now();
  const result = await parser.parse(
    { match_id: id, demo_key: match.demo_key, parse_id: parse.id },
    { attempt, requestId: message.id },
  );
  const durationMs = Date.now() - started;
  (result.kind === "ok" ? console.log : console.warn)(
    JSON.stringify({
      event: "parse_call",
      match_id: id,
      parse_id: parse.id,
      build,
      ...log,
      max_attempts: MAX_ATTEMPTS,
      final: attempt >= MAX_ATTEMPTS,
      duration_ms: durationMs,
      status: result.status ?? null,
      result: result.kind,
      ...(result.kind !== "ok" ? { error: result.error } : {}),
    }),
  );
  await addContainerTime(env.DB, started, durationMs);
  await addEvent(env.DB, id, {
    kind: "attempt",
    msg: `${reparse ? "reparse " : ""}${outcome(result, durationMs)}`,
    attempt,
    build,
    parse_id: parse.id,
  });

  // Final failures: a parse fails the match, a reparse only records why.
  const fail = async (error: string) => {
    if (!reparse) {
      await setStatus(env.DB, id, "failed", { error, log });
      return;
    }
    await setReparseError(env.DB, id, error, build);
    console.warn(JSON.stringify({ event: "reparse_failed", match_id: id, build, ...log, error }));
  };

  switch (result.kind) {
    case "ok": {
      let file: MatchFile;
      try {
        file = asMatchFile(result.body);
      } catch (err) {
        // Retrying would get the same output back, so this is final.
        await fail(failure("bad_output", errorText(err)));
        message.ack();
        return;
      }
      if (!(await saveParsedMatch(env.DB, id, file, parse, Date.now(), log))) {
        console.warn(
          JSON.stringify({ event: "parse_superseded", match_id: id, parse_id: parse.id, started_from: parse.from }),
        );
      }
      message.ack();
      return;
    }
    case "rejected":
      await fail(failure(result.code, result.error));
      message.ack();
      return;
    case "transient":
      // Back in the queue until the retry. After the last attempt the queue
      // moves the message to the dead letter queue instead.
      if (reparse) await setReparseError(env.DB, id, result.error, build);
      else await setStatus(env.DB, id, "queued", { error: result.error, log });
      message.retry();
      return;
  }
}

// Messages land here once every attempt has failed. A parse fails its match
// and a reparse records why, keeping the last error the consumer recorded.
// A match that was ever ready stays ready.
export async function handleDeadLetters(batch: MessageBatch<ParseJob>, env: Env): Promise<void> {
  for (const message of batch.messages) {
    const id = message.body?.match_id;
    const reparse = message.body?.reparse === true;
    const match = typeof id === "string" && isMatchId(id) ? await getMatch(env.DB, id) : null;
    const lastError = (reparse ? match?.reparse_error : match?.error) ?? null;
    console.error(
      JSON.stringify({
        event: "dead_letter",
        match_id: id ?? null,
        reparse,
        attempts: message.attempts,
        status: match?.status ?? null,
        last_error: lastError,
      }),
    );
    if (match) {
      const error = failure("parse_failed", `failed after ${MAX_ATTEMPTS} attempts: ${lastError ?? "unknown error"}`);
      if (reparse) {
        await setReparseError(env.DB, match.id, error, env.PARSER_BUILD);
        await addEvent(env.DB, match.id, { kind: "reparse", msg: `gave up: ${error}` });
      } else {
        await setStatus(env.DB, match.id, "failed", { error, from: ["queued", "parsing"] });
      }
    }
    message.ack();
  }
}

// How an attempt ended, for the match's history.
function outcome(result: ParseResult, durationMs: number): string {
  const took = `in ${(durationMs / 1000).toFixed(1)} s`;
  switch (result.kind) {
    case "ok":
      return `parsed ${took}`;
    case "rejected":
      return `rejected ${took}: ${failure(result.code, result.error)}`;
    case "transient":
      return `failed ${took}: ${result.error}`;
  }
}
