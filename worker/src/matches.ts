// D1 access for the spec's four tables. The Worker is the only writer.

import { addEvent } from "./events";
import type { MatchFile } from "./match-file";

export type MatchStatus = "uploaded" | "queued" | "parsing" | "ready" | "failed";

export interface MatchRow {
  id: string;
  status: MatchStatus;
  map: string | null;
  score_a: number | null;
  score_b: number | null;
  demo_key: string;
  demo_sha256: string | null;
  parser_version: string | null;
  schema_version: number | null;
  error: string | null;
  created_at: number;
  parsed_at: number | null;
  live_parse_id: string | null;
  attempted_build: string | null;
  reparse_error: string | null;
  reparse_build: string | null;
  status_changed_at: number | null;
  featured_at: number | null;
}

// One card of the home page gallery. Only these columns, so names and Steam
// ids can never reach it.
export interface GalleryMatch {
  id: string;
  map: string | null;
  score_a: number | null;
  score_b: number | null;
  parsed_at: number;
}

export interface PlayerRow {
  idx: number;
  name: string | null;
  team: string | null;
  kills: number | null;
  deaths: number | null;
  assists: number | null;
  adr: number | null;
  hs_pct: number | null;
}

export interface RoundRow {
  n: number;
  winner: string | null;
  reason: string | null;
  side_a: string | null;
}

// Timestamps are Unix milliseconds.
export async function insertMatch(
  db: D1Database,
  id: string,
  demoKey: string,
  now: number,
): Promise<void> {
  await db
    .prepare(
      "INSERT INTO matches (id, status, demo_key, created_at, status_changed_at) VALUES (?1, 'uploaded', ?2, ?3, ?3)",
    )
    .bind(id, demoKey, now)
    .run();
  await statusChanged(db, id, "uploaded");
}

export type Claim =
  | { kind: "claimed" }
  // Another match that hasn't failed already has this demo.
  | { kind: "duplicate"; match: MatchRow }
  // The match left `uploaded` first, through a concurrent confirm.
  | { kind: "lost" };

// Moves an uploaded match to queued and stores its demo's hash, unless a match
// that hasn't failed already has the same demo. It's one statement, so of two
// confirms of the same demo, only one can claim it.
export async function claimUpload(
  db: D1Database,
  id: string,
  sha256: string,
  log: Record<string, unknown> = {},
): Promise<Claim> {
  const claimed = await db
    .prepare(
      `UPDATE matches SET status = 'queued', demo_sha256 = ?2, status_changed_at = ?3
       WHERE id = ?1 AND status = 'uploaded' AND NOT EXISTS (
         SELECT 1 FROM matches WHERE demo_sha256 = ?2 AND status != 'failed' AND id != ?1)`,
    )
    .bind(id, sha256, Date.now())
    .run();
  if (claimed.meta.changes > 0) {
    await statusChanged(db, id, "queued", log);
    return { kind: "claimed" };
  }
  const original = await db
    .prepare(
      "SELECT * FROM matches WHERE demo_sha256 = ? AND status != 'failed' AND id != ? ORDER BY created_at LIMIT 1",
    )
    .bind(sha256, id)
    .first<MatchRow>();
  return original ? { kind: "duplicate", match: original } : { kind: "lost" };
}

// Removes a match that never got past `uploaded`, and its history.
export async function deleteUploadedMatch(db: D1Database, id: string): Promise<void> {
  await db.batch([
    db.prepare("DELETE FROM matches WHERE id = ? AND status = 'uploaded'").bind(id),
    db
      .prepare("DELETE FROM match_events WHERE match_id = ?1 AND NOT EXISTS (SELECT 1 FROM matches WHERE id = ?1)")
      .bind(id),
  ]);
}

export function getMatch(db: D1Database, id: string): Promise<MatchRow | null> {
  return db.prepare("SELECT * FROM matches WHERE id = ?").bind(id).first<MatchRow>();
}

// Everything the match page needs, in one round trip. Steam ids stay out of
// the summary; match.json has them.
export async function getMatchPage(
  db: D1Database,
  id: string,
): Promise<{ match: MatchRow; players: PlayerRow[]; rounds: RoundRow[] } | null> {
  const [match, players, rounds] = await db.batch([
    db.prepare("SELECT * FROM matches WHERE id = ?").bind(id),
    db
      .prepare(
        "SELECT idx, name, team, kills, deaths, assists, adr, hs_pct FROM players WHERE match_id = ? ORDER BY idx",
      )
      .bind(id),
    db
      .prepare("SELECT n, winner, reason, side_a FROM rounds WHERE match_id = ? ORDER BY n")
      .bind(id),
  ]);
  const row = match?.results[0] as MatchRow | undefined;
  if (!row) return null;
  return {
    match: row,
    players: (players?.results ?? []) as PlayerRow[],
    rounds: (rounds?.results ?? []) as RoundRow[],
  };
}

// Lists the match in the gallery from `at` (Unix ms), or unlists it with null.
export async function setFeatured(db: D1Database, id: string, at: number | null): Promise<void> {
  await db.prepare("UPDATE matches SET featured_at = ? WHERE id = ?").bind(at, id).run();
}

// Featured matches that are ready, the most recently featured first.
export async function listGallery(db: D1Database, limit = 24): Promise<GalleryMatch[]> {
  const { results } = await db
    .prepare(
      `SELECT id, map, score_a, score_b, parsed_at FROM matches
       WHERE featured_at IS NOT NULL AND status = 'ready' ORDER BY featured_at DESC LIMIT ?`,
    )
    .bind(limit)
    .all<GalleryMatch>();
  return results;
}

// The parse a job writes, and the live parse it started from.
export interface ParseTarget {
  id: string;
  from: string | null;
}

// Makes the parse live: points the match at it and replaces its players,
// rounds and kills, in one transaction. The pointer only moves if it still
// points where this parse started from, so a parse that a newer one beat
// changes nothing. Returns whether it went live.
//
// Each table gets one INSERT that reads a JSON array through json_each. D1
// counts every statement in a batch toward its per-invocation query limit (50
// on the Free plan), so a statement per kill would not fit.
export async function saveParsedMatch(
  db: D1Database,
  id: string,
  file: MatchFile,
  parse: ParseTarget,
  now: number,
  log: Record<string, unknown> = {},
): Promise<boolean> {
  const scores = new Map(file.scoreboard.map((s) => [s.player, s]));
  const players = file.players.map((p) => ({ ...scores.get(p.idx), ...p }));
  const scoreA = file.rounds.filter((r) => r.winner === "A").length;
  const scoreB = file.rounds.filter((r) => r.winner === "B").length;
  // ?1 is the match id and ?2 this parse's id in every statement below.
  const isLive = "(SELECT live_parse_id FROM matches WHERE id = ?1) = ?2";
  const field = (name: string) => `json_extract(value, '$.${name}')`;
  const insertFrom = (table: string, columns: string[]) =>
    `INSERT INTO ${table} (match_id, ${columns.join(", ")}) ` +
    `SELECT ?1, ${columns.map(field).join(", ")} FROM json_each(?3) WHERE ${isLive}`;
  const [swap] = await db.batch([
    db
      .prepare(
        `UPDATE matches SET live_parse_id = ?2, status = 'ready', error = NULL,
           status_changed_at = CASE status WHEN 'ready' THEN status_changed_at ELSE ?3 END,
           map = ?4, score_a = ?5, score_b = ?6, demo_sha256 = ?7, parser_version = ?8,
           schema_version = ?9, parsed_at = ?3, reparse_error = NULL, reparse_build = NULL
         WHERE id = ?1 AND live_parse_id IS ?10`,
      )
      .bind(
        id,
        parse.id,
        now,
        file.match.map,
        scoreA,
        scoreB,
        file.match.demo_sha256 ?? null,
        file.parser_version,
        file.schema_version,
        parse.from,
      ),
    db.prepare(`DELETE FROM players WHERE match_id = ?1 AND ${isLive}`).bind(id, parse.id),
    db.prepare(`DELETE FROM rounds WHERE match_id = ?1 AND ${isLive}`).bind(id, parse.id),
    db.prepare(`DELETE FROM kills WHERE match_id = ?1 AND ${isLive}`).bind(id, parse.id),
    db
      .prepare(
        insertFrom("players", ["idx", "steam_id", "name", "team", "kills", "deaths", "assists", "adr", "hs_pct"]),
      )
      .bind(id, parse.id, JSON.stringify(players)),
    db
      .prepare(insertFrom("rounds", ["n", "winner", "reason", "side_a"]))
      .bind(id, parse.id, JSON.stringify(file.rounds)),
    // json_extract turns the JSON booleans in headshot into 1 and 0.
    db
      .prepare(insertFrom("kills", ["round", "tick", "attacker", "victim", "weapon", "headshot"]))
      .bind(id, parse.id, JSON.stringify(file.kills)),
  ]);
  if (!swap?.meta.changes) return false;
  await statusChanged(db, id, "ready", {
    ...log,
    parse_id: parse.id,
    parser_version: file.parser_version,
    players: file.players.length,
    rounds: file.rounds.length,
    kills: file.kills.length,
    score_a: scoreA,
    score_b: scoreB,
  });
  return true;
}

// Notes the build a parse attempt runs, before it runs.
export async function recordAttempt(db: D1Database, id: string, build: string): Promise<void> {
  await db.prepare("UPDATE matches SET attempted_build = ? WHERE id = ?").bind(build, id).run();
}

// A reparse that fails leaves the match ready on its live parse, with the
// error and the build that hit it kept beside it. A later success clears them.
export async function setReparseError(db: D1Database, id: string, error: string, build: string): Promise<void> {
  await db
    .prepare("UPDATE matches SET reparse_error = ?, reparse_build = ? WHERE id = ?")
    .bind(error, build, id)
    .run();
}

// A failed match's error is "<code>: <detail>", so the match page can explain
// each code. The parser's 422 codes (bad_demo, demo_not_found) pass through;
// the Worker adds too_large, bad_output, parse_failed and stuck.
export function failure(code: string, detail: string): string {
  return `${code}: ${detail}`;
}

export interface StatusChange {
  // Written to the error column. Left unchanged when undefined.
  error?: string | null;
  // Only change the status if it is currently one of these.
  from?: readonly MatchStatus[];
  // Extra fields for the match_status log line, such as the attempt. A
  // `note` also goes into the match's history.
  log?: Record<string, unknown>;
}

// Sets a match's status, logs the change and adds it to the match's history.
// Returns false if no row changed, either because the match is missing or its
// status was not in `from`.
export async function setStatus(
  db: D1Database,
  id: string,
  status: MatchStatus,
  change: StatusChange = {},
): Promise<boolean> {
  const sets = ["status = ?", "status_changed_at = ?"];
  const params: (string | number | null)[] = [status, Date.now()];
  if (change.error !== undefined) {
    sets.push("error = ?");
    params.push(change.error);
  }
  let sql = `UPDATE matches SET ${sets.join(", ")} WHERE id = ?`;
  params.push(id);
  if (change.from) {
    sql += ` AND status IN (${change.from.map(() => "?").join(", ")})`;
    params.push(...change.from);
  }
  const result = await db.prepare(sql).bind(...params).run();
  const changed = result.meta.changes > 0;
  if (changed) await statusChanged(db, id, status, { ...change.log, error: change.error });
  return changed;
}

// Every status change is one JSON line, per the spec's observability section,
// and one row of the match's history. The log level follows the outcome, so
// the Workers Logs level filter finds failures.
async function statusChanged(
  db: D1Database,
  matchId: string,
  status: MatchStatus,
  fields: Record<string, unknown> = {},
): Promise<void> {
  const { error, ...rest } = fields;
  const log = status === "failed" ? console.error : error ? console.warn : console.log;
  log(JSON.stringify({ event: "match_status", match_id: matchId, status, ...rest, ...(error ? { error } : {}) }));

  const { attempt, note, parse_id } = fields;
  let msg: string = status;
  if (typeof note === "string") msg += ` (${note})`;
  if (typeof error === "string") msg += `: ${error}`;
  await addEvent(db, matchId, {
    kind: "status",
    msg,
    attempt: typeof attempt === "number" ? attempt : undefined,
    parse_id: typeof parse_id === "string" ? parse_id : undefined,
  });
}
