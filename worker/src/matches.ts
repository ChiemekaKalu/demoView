// D1 access for the spec's four tables. The Worker is the only writer.

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
    .prepare("INSERT INTO matches (id, status, demo_key, created_at) VALUES (?, 'uploaded', ?, ?)")
    .bind(id, demoKey, now)
    .run();
  logStatus(id, "uploaded");
}

export function getMatch(db: D1Database, id: string): Promise<MatchRow | null> {
  return db.prepare("SELECT * FROM matches WHERE id = ?").bind(id).first<MatchRow>();
}

// Everything the match page needs, in one round trip. Steam ids stay out of
// the public response until the spec's open question on them is settled.
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

// Replaces the match's players, rounds and kills with the parse result and
// marks it ready, in one transaction, so a reparse overwrites cleanly.
//
// Each table gets one INSERT that reads a JSON array through json_each. D1
// counts every statement in a batch toward its per-invocation query limit (50
// on the Free plan), so a statement per kill would not fit.
export async function saveParsedMatch(
  db: D1Database,
  id: string,
  file: MatchFile,
  now: number,
): Promise<void> {
  const scores = new Map(file.scoreboard.map((s) => [s.player, s]));
  const players = file.players.map((p) => ({ ...scores.get(p.idx), ...p }));
  const scoreA = file.rounds.filter((r) => r.winner === "A").length;
  const scoreB = file.rounds.filter((r) => r.winner === "B").length;
  const field = (name: string) => `json_extract(value, '$.${name}')`;
  const insertFrom = (table: string, columns: string[]) =>
    `INSERT INTO ${table} (match_id, ${columns.join(", ")}) ` +
    `SELECT ?1, ${columns.map(field).join(", ")} FROM json_each(?2)`;
  await db.batch([
    db.prepare("DELETE FROM players WHERE match_id = ?").bind(id),
    db.prepare("DELETE FROM rounds WHERE match_id = ?").bind(id),
    db.prepare("DELETE FROM kills WHERE match_id = ?").bind(id),
    db
      .prepare(
        insertFrom("players", [
          "idx",
          "steam_id",
          "name",
          "team",
          "kills",
          "deaths",
          "assists",
          "adr",
          "hs_pct",
        ]),
      )
      .bind(id, JSON.stringify(players)),
    db
      .prepare(insertFrom("rounds", ["n", "winner", "reason", "side_a"]))
      .bind(id, JSON.stringify(file.rounds)),
    // json_extract turns the JSON booleans in headshot into 1 and 0.
    db
      .prepare(insertFrom("kills", ["round", "tick", "attacker", "victim", "weapon", "headshot"]))
      .bind(id, JSON.stringify(file.kills)),
    db
      .prepare(
        `UPDATE matches SET status = 'ready', error = NULL, map = ?, score_a = ?, score_b = ?,
           demo_sha256 = ?, parser_version = ?, schema_version = ?, parsed_at = ?
         WHERE id = ?`,
      )
      .bind(
        file.match.map,
        scoreA,
        scoreB,
        file.match.demo_sha256 ?? null,
        file.parser_version,
        file.schema_version,
        now,
        id,
      ),
  ]);
  logStatus(id, "ready");
}

export interface StatusChange {
  // Written to the error column. Left unchanged when undefined.
  error?: string | null;
  // Only change the status if it is currently one of these.
  from?: readonly MatchStatus[];
}

// Sets a match's status and logs the change. Returns false if no row changed,
// either because the match is missing or its status was not in `from`.
export async function setStatus(
  db: D1Database,
  id: string,
  status: MatchStatus,
  change: StatusChange = {},
): Promise<boolean> {
  const sets = ["status = ?"];
  const params: (string | null)[] = [status];
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
  if (changed) logStatus(id, status, change.error);
  return changed;
}

// One JSON line per status change, per the spec's observability section.
export function logStatus(matchId: string, status: MatchStatus, error?: string | null): void {
  console.log(
    JSON.stringify({ event: "match_status", match_id: matchId, status, ...(error ? { error } : {}) }),
  );
}
