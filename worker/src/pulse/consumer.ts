// The pulse queue: after a parse goes live, Jev reads every round at every
// moment, and the result is written as parsed/{id}/{parse_id}/pulse.json. It
// has its own queue, so Jev being slow, down or out of credits never touches a
// match's parse or its ready status. See .claude/ideas/jev/spec.md.

import { addEvent } from "../events";
import { isMatchId, isParseId, matchFileKey, parsedPrefix, roundKey } from "../keys";
import { errorText } from "../log";
import { getMatch } from "../matches";
import { addAiUsage, aiTokens } from "../usage";
import { type JevClient, jevClient, JevError, Ledger } from "./jev";
import {
  FALLBACK_MODEL,
  fallbackRound,
  grade,
  jevRound,
  MOMENT_QUESTIONS,
  type PulseFile,
  pulseFile,
  ROUND_QUESTIONS,
} from "./model";
import { type PulseMatch, type PulseRound, RoundView, type Side, STATE_VERSION } from "./state";

export interface PulseJob {
  match_id: string;
  parse_id: string;
}

// The account's AI Gateway, which carries the Unified Billing credit.
const GATEWAY = "default";
// A long match is about 220 calls; anything far past that is a bug.
const MAX_CALLS = 400;
// About $2 of Jev a month at $0.042 per 1M input tokens, a few hundred
// matches. Past it, Pulse falls back to the free model until the month ends.
const AI_TOKENS_PER_MONTH = 50_000_000;
// Must match max_retries + 1 for the pulse queue in wrangler.jsonc. After the
// last attempt the fallback model draws the line.
export const PULSE_ATTEMPTS = 3;

export type JevFactory = (ledger: Ledger) => JevClient;

// Queues a Pulse for a parse that just went live, when Pulse is on. A failure
// here never fails the parse.
export async function queuePulse(env: Env, matchId: string, parseId: string): Promise<void> {
  if (env.PULSE_ENABLED !== "true") return;
  try {
    await env.PULSE_QUEUE.send({ match_id: matchId, parse_id: parseId } satisfies PulseJob);
  } catch (err) {
    console.error(JSON.stringify({ event: "pulse_queue_failed", match_id: matchId, parse_id: parseId, error: errorText(err) }));
  }
}

export async function handlePulseBatch(
  batch: MessageBatch<PulseJob>,
  env: Env,
  makeJev: JevFactory = (ledger) => jevClient(env.AI, GATEWAY, ledger),
): Promise<void> {
  for (const message of batch.messages) {
    try {
      await handlePulseJob(message, env, makeJev);
    } catch (err) {
      console.error(
        JSON.stringify({ event: "pulse_error", match_id: message.body?.match_id ?? null, error: errorText(err) }),
      );
      message.retry();
    }
  }
}

async function handlePulseJob(message: Message<PulseJob>, env: Env, makeJev: JevFactory): Promise<void> {
  const { match_id: id, parse_id: parseId } = message.body ?? ({} as Partial<PulseJob>);
  const valid = typeof id === "string" && isMatchId(id) && typeof parseId === "string" && isParseId(parseId);
  const match = valid ? await getMatch(env.DB, id) : null;
  // Only the live parse gets a Pulse. A newer parse queues its own.
  if (!valid || !match || match.status !== "ready" || match.live_parse_id !== parseId) {
    console.log(JSON.stringify({ event: "pulse_skipped", match_id: id ?? null, parse_id: parseId ?? null }));
    message.ack();
    return;
  }
  const key = `${parsedPrefix(id, parseId)}pulse.json`;
  if (await env.BUCKET.head(key)) {
    message.ack(); // an earlier delivery wrote it
    return;
  }

  const views = await readRounds(env.BUCKET, id, parseId);
  const calls = views.reduce((sum, v) => sum + v.cuts.length + 1, 0);
  const now = Date.now();
  const ledger = new Ledger();
  let result = "ok";
  let built: Built;
  if (calls > MAX_CALLS) {
    result = "fallback:size";
    built = fallback(views, parseId);
  } else if ((await aiTokens(env.DB, now)) >= AI_TOKENS_PER_MONTH) {
    result = "fallback:cap";
    built = fallback(views, parseId);
  } else {
    try {
      built = await askJev(views, makeJev(ledger), parseId);
    } catch (err) {
      const failure = err instanceof JevError ? err : new JevError(errorText(err), "other");
      // Out of credits won't get better on a retry. Anything else gets
      // another try later, and the fallback model draws the last one.
      if (failure.kind !== "credits" && message.attempts < PULSE_ATTEMPTS) {
        await addAiUsage(env.DB, now, ledger.calls, ledger.inTokens);
        logPulse(id, parseId, `retry:${failure.kind}`, null, ledger, now, failure.message);
        message.retry();
        return;
      }
      result = `fallback:${failure.kind}`;
      built = fallback(views, parseId);
    }
  }

  await env.BUCKET.put(key, JSON.stringify(built.file), { httpMetadata: { contentType: "application/json" } });
  await addAiUsage(env.DB, now, ledger.calls, ledger.inTokens);
  if (built.file.model !== FALLBACK_MODEL) await saveQuality(env.DB, id, built, views, now);
  await addEvent(env.DB, id, {
    kind: "pulse",
    msg: `${built.file.model}, ${result}: ${ledger.calls} calls, $${ledger.usd.toFixed(4)}`,
    parse_id: parseId,
  });
  logPulse(id, parseId, result, built.file.model, ledger, now);
  message.ack();
}

interface Built {
  file: PulseFile;
  howWonAgreed: number;
  headlinesDropped: number;
}

// Every round at every moment, and each round whole, through Jev.
async function askJev(views: RoundView[], jev: JevClient, parseId: string): Promise<Built> {
  let model = "";
  let howWonAgreed = 0;
  let headlinesDropped = 0;
  const rounds = await Promise.all(
    views.map(async (view) => {
      const [moments, whole] = await Promise.all([
        Promise.all(view.cuts.map((cut) => jev.ask(view.state({ cut }), MOMENT_QUESTIONS))),
        jev.ask(view.state({ outcome: true, ids: true }), ROUND_QUESTIONS),
      ]);
      model = whole.model;
      const round = jevRound(view, moments.map((m) => m.answers), whole.answers);
      const howWon = whole.answers.how_won;
      if (howWon?.type === "choice" && howWon.choice === round.facts.how_won) howWonAgreed++;
      if (whole.answers.headline && round.headline === null) headlinesDropped++;
      return round;
    }),
  );
  return { file: pulseFile(model, parseId, rounds), howWonAgreed, headlinesDropped };
}

function fallback(views: RoundView[], parseId: string): Built {
  return { file: pulseFile(FALLBACK_MODEL, parseId, views.map(fallbackRound)), howWonAgreed: 0, headlinesDropped: 0 };
}

// The parse's match.json and round files, as one view per round Jev reads.
// A round that ended in freeze time, such as a surrender, has no moments.
async function readRounds(bucket: R2Bucket, id: string, parseId: string): Promise<RoundView[]> {
  const matchObject = await bucket.get(matchFileKey(id, parseId));
  if (!matchObject) throw new Error("the live parse has no match.json");
  const match = await matchObject.json<PulseMatch>();
  const views: RoundView[] = [];
  for (const r of match.rounds) {
    if (r.reason === "surrender") continue;
    const file = await bucket.get(roundKey(id, parseId, r.n));
    views.push(new RoundView(match, r.n, file ? await file.json<PulseRound>() : null));
  }
  return views;
}

async function saveQuality(db: D1Database, id: string, built: Built, views: RoundView[], now: number): Promise<void> {
  const winners = new Map<number, Side>(views.map((v) => [v.n, v.winner]));
  const { moments, brier, logloss } = grade(built.file, winners);
  await db
    .prepare(
      `INSERT OR REPLACE INTO pulse_quality (match_id, parse_id, model, state_version, moments, brier, logloss,
         rounds, how_won_agreed, headlines_dropped, created_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    )
    .bind(
      id,
      built.file.parse_id,
      built.file.model,
      STATE_VERSION,
      moments,
      brier,
      logloss,
      built.file.rounds.length,
      built.howWonAgreed,
      built.headlinesDropped,
      now,
    )
    .run();
}

// One line per Pulse: what it cost and how the calls went.
function logPulse(
  id: string,
  parseId: string,
  result: string,
  model: string | null,
  ledger: Ledger,
  started: number,
  error?: string,
): void {
  const line = {
    event: "pulse",
    match_id: id,
    parse_id: parseId,
    model,
    result,
    calls: ledger.calls,
    errors: ledger.errors,
    retries: ledger.retries,
    http_429: ledger.rateLimited,
    in_tokens: ledger.inTokens,
    usd: Math.round(ledger.usd * 1e6) / 1e6,
    p50_ms: ledger.percentile(0.5),
    p95_ms: ledger.percentile(0.95),
    wall_ms: Date.now() - started,
    ...(error ? { error } : {}),
  };
  (result === "ok" ? console.log : console.warn)(JSON.stringify(line));
}
