// The Worker's scheduled jobs, keyed by the cron expressions in
// wrangler.jsonc: the stuck-match sweeper, the reparse batch and cleanup.

import type { ParseJob } from "./api";
import { addEvent } from "./events";
import { isParseId, matchFileKey, parsedPrefix, parseIdTime } from "./keys";
import { deleteUploadedMatch, failure, setStatus } from "./matches";
import { parserBuild } from "./parser";
import { CONTAINER_MS_PER_MONTH, containerTime } from "./usage";

export const SWEEP_CRON = "*/10 * * * *";
export const REPARSE_CRON = "0 * * * *";
export const CLEANUP_CRON = "30 4 * * *";

// A match this long in queued or parsing since its last status change has
// lost its queue message; one attempt ends well before this.
const STUCK_MS = 30 * 60 * 1000;

const REPARSE_BATCH = 5;
// Reparses stop here, leaving the rest of the allowance for uploads.
const REPARSE_CAP_MS = 0.8 * CONTAINER_MS_PER_MONTH;

// Parses nothing points at are kept this long, for viewers still on them.
const KEEP_MS = 7 * 24 * 60 * 60 * 1000;
// An upload not confirmed by now never will be: its URL expired after an hour.
const ABANDONED_MS = 24 * 60 * 60 * 1000;
const CLEANUP_BATCH = 50;

// The jobs by name, for running one by hand (POST /api/admin/cron/:job).
export const JOBS = new Map<string, (env: Env, now: number) => Promise<void>>([
  ["sweep", sweep],
  ["reparse", reparseBatch],
  ["cleanup", cleanup],
]);

const JOB_BY_CRON = new Map([
  [SWEEP_CRON, "sweep"],
  [REPARSE_CRON, "reparse"],
  [CLEANUP_CRON, "cleanup"],
]);

export async function runCron(cron: string, env: Env, now: number): Promise<void> {
  const job = JOBS.get(JOB_BY_CRON.get(cron) ?? "");
  if (!job) return console.error(JSON.stringify({ event: "cron_unknown", cron }));
  return job(env, now);
}

// Requeues a match stuck in queued, whose queue message is gone, and fails
// one stuck in parsing, which nothing will finish. A ready match is never
// touched. A requeue that turns out to be a duplicate is harmless: the
// consumer skips a match that isn't queued.
export async function sweep(env: Env, now: number): Promise<void> {
  const { results } = await env.DB.prepare(
    "SELECT id, status FROM matches WHERE status IN ('queued', 'parsing') AND status_changed_at < ?",
  )
    .bind(now - STUCK_MS)
    .all<{ id: string; status: "queued" | "parsing" }>();
  for (const { id, status } of results) {
    const log = { swept: true, note: `no change for 30 minutes in ${status}` };
    if (status === "queued") {
      if (await setStatus(env.DB, id, "queued", { from: ["queued"], log })) {
        await env.PARSE_QUEUE.send({ match_id: id } satisfies ParseJob);
      }
    } else {
      await setStatus(env.DB, id, "failed", {
        error: failure("stuck", "no result after 30 minutes of parsing"),
        from: ["parsing"],
        log,
      });
    }
  }
}

// Queues reparses of up to REPARSE_BATCH ready matches whose latest attempt
// ran an older parser build, oldest match first. It waits while anything is
// queued or parsing, so an upload waits behind at most one batch, and stops
// for the month at the container cap.
export async function reparseBatch(env: Env, now: number): Promise<void> {
  const build = parserBuild(env);
  const skip = (reason: string, fields: Record<string, unknown> = {}) =>
    console.log(JSON.stringify({ event: "reparse_batch", build, skipped: reason, ...fields }));
  // A deploy without CI's build id can't tell old parses from new.
  if (env.PARSER_BUILD === "dev") return skip("no build id");

  const busy = await env.DB.prepare("SELECT count(*) AS n FROM matches WHERE status IN ('queued', 'parsing')").first<number>(
    "n",
  );
  if (busy) return skip("busy", { busy });
  const used = await containerTime(env.DB, now);
  if (used >= REPARSE_CAP_MS) return skip("container cap", { container_ms: used });

  const { results } = await env.DB.prepare(
    "SELECT id FROM matches WHERE status = 'ready' AND attempted_build IS NOT ? ORDER BY created_at LIMIT ?",
  )
    .bind(build, REPARSE_BATCH)
    .all<{ id: string }>();
  if (results.length === 0) return;

  await env.PARSE_QUEUE.sendBatch(results.map(({ id }) => ({ body: { match_id: id, reparse: true } satisfies ParseJob })));
  // Marked as attempted now, so the next batch doesn't pick them again
  // before the consumer gets to them.
  await env.DB.batch(
    results.map(({ id }) => env.DB.prepare("UPDATE matches SET attempted_build = ? WHERE id = ?").bind(build, id)),
  );
  for (const { id } of results) {
    await addEvent(env.DB, id, { kind: "reparse", msg: "queued by the hourly reparse", build }, now);
  }
  console.log(JSON.stringify({ event: "reparse_batch", build, queued: results.length, container_ms: used }));
}

// Deletes the files of parses that aren't live and are over KEEP_MS old, a
// reparsed match's v1 files once its live parse is that old, and uploads that
// were never confirmed. Parse prefixes come from the attempts in the match
// history. v1 files are deleted by key: they sit directly under parsed/{id}/,
// the parent of every parse.
export async function cleanup(env: Env, now: number): Promise<void> {
  const cutoff = now - KEEP_MS;
  const stale = await env.DB.prepare(
    `SELECT DISTINCT e.match_id, e.parse_id FROM match_events e JOIN matches m ON m.id = e.match_id
     WHERE e.kind = 'attempt' AND e.parse_id IS NOT NULL AND e.ts < ?1
       AND e.parse_id IS NOT m.live_parse_id
       AND NOT EXISTS (SELECT 1 FROM match_events c
                       WHERE c.match_id = e.match_id AND c.kind = 'cleanup' AND c.parse_id = e.parse_id)
     LIMIT ?2`,
  )
    .bind(cutoff, CLEANUP_BATCH)
    .all<{ match_id: string; parse_id: string }>();
  for (const { match_id, parse_id } of stale.results) {
    if (!isParseId(parse_id)) continue;
    const keys = await listKeys(env.BUCKET, parsedPrefix(match_id, parse_id));
    await deleteKeys(env.BUCKET, keys);
    await addEvent(env.DB, match_id, { kind: "cleanup", msg: `removed ${keys.length} files`, parse_id }, now);
  }

  // Matches created before the first attempt in any history may have v1
  // files; later ones never do.
  const v1 = await env.DB.prepare(
    `SELECT id, live_parse_id FROM matches m
     WHERE live_parse_id IS NOT NULL
       AND created_at < (SELECT MIN(ts) FROM match_events WHERE kind = 'attempt')
       AND NOT EXISTS (SELECT 1 FROM match_events c WHERE c.match_id = m.id AND c.kind = 'cleanup' AND c.parse_id IS NULL)
     LIMIT ?`,
  )
    .bind(CLEANUP_BATCH)
    .all<{ id: string; live_parse_id: string }>();
  for (const { id, live_parse_id } of v1.results) {
    if (parseIdTime(live_parse_id) >= cutoff) continue;
    const rounds = await listKeys(env.BUCKET, `${parsedPrefix(id, null)}rounds/`);
    const match = (await env.BUCKET.head(matchFileKey(id, null))) ? [matchFileKey(id, null)] : [];
    await deleteKeys(env.BUCKET, [...match, ...rounds]);
    await addEvent(env.DB, id, { kind: "cleanup", msg: `removed ${match.length + rounds.length} v1 files` }, now);
  }
  const abandoned = await env.DB.prepare(
    "SELECT id, demo_key FROM matches WHERE status = 'uploaded' AND created_at < ? LIMIT ?",
  )
    .bind(now - ABANDONED_MS, CLEANUP_BATCH)
    .all<{ id: string; demo_key: string }>();
  for (const { id, demo_key } of abandoned.results) {
    await env.BUCKET.delete(demo_key);
    await deleteUploadedMatch(env.DB, id);
  }
  console.log(
    JSON.stringify({
      event: "cleanup",
      parses: stale.results.length,
      v1_matches: v1.results.length,
      abandoned_uploads: abandoned.results.length,
    }),
  );
}

async function listKeys(bucket: R2Bucket, prefix: string): Promise<string[]> {
  const keys: string[] = [];
  let cursor: string | undefined;
  do {
    const page = await bucket.list({ prefix, cursor });
    keys.push(...page.objects.map((o) => o.key));
    cursor = page.truncated ? page.cursor : undefined;
  } while (cursor);
  return keys;
}

// R2 deletes at most 1,000 keys per call.
async function deleteKeys(bucket: R2Bucket, keys: string[]): Promise<void> {
  for (let i = 0; i < keys.length; i += 1000) await bucket.delete(keys.slice(i, i + 1000));
}
