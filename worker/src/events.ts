// A match's pipeline history (match_events): status changes, parse attempts,
// reparse requests, Pulses and cleanups. Workers Logs keep 7 days; this
// keeps the rest. Rows count from 1 within a match.

export type EventKind = "status" | "attempt" | "reparse" | "pulse" | "cleanup";

export interface MatchEvent {
  kind: EventKind;
  msg: string;
  attempt?: number;
  build?: string;
  parse_id?: string | null;
}

export interface EventRow extends Required<MatchEvent> {
  seq: number;
  ts: number;
}

export async function addEvent(db: D1Database, matchId: string, event: MatchEvent, now = Date.now()): Promise<void> {
  await db
    .prepare(
      `INSERT INTO match_events (match_id, seq, ts, kind, msg, attempt, build, parse_id)
       SELECT ?1, COALESCE(MAX(seq), 0) + 1, ?2, ?3, ?4, ?5, ?6, ?7 FROM match_events WHERE match_id = ?1`,
    )
    .bind(matchId, now, event.kind, event.msg, event.attempt ?? null, event.build ?? null, event.parse_id ?? null)
    .run();
}

export async function getEvents(db: D1Database, matchId: string): Promise<EventRow[]> {
  const { results } = await db
    .prepare(
      "SELECT seq, ts, kind, msg, attempt, build, parse_id FROM match_events WHERE match_id = ? ORDER BY seq",
    )
    .bind(matchId)
    .all<EventRow>();
  return results;
}
