// Fixtures mode: builds what the API would return from a match.json, the
// same way the Worker fills D1 from it (worker/src/matches.ts
// saveParsedMatch): players merged with their scoreboard row, and the score
// counted from round winners.

import type { MatchFile, MatchSummary } from './types'

export function deriveSummary(file: MatchFile, id: string): MatchSummary {
  const scores = new Map(file.scoreboard.map((s) => [s.player, s]))
  return {
    id,
    status: 'ready',
    error: null,
    map: file.match.map,
    score_a: file.rounds.filter((r) => r.winner === 'A').length,
    score_b: file.rounds.filter((r) => r.winner === 'B').length,
    parser_version: file.parser_version,
    schema_version: file.schema_version,
    created_at: 0,
    parsed_at: 0,
    parse_id: null,
    // Steam ids stay out, as in the Worker's public response.
    players: [...file.players]
      .sort((a, b) => a.idx - b.idx)
      .map((p) => {
        const s = scores.get(p.idx)
        return {
          idx: p.idx,
          name: p.name,
          team: p.team,
          kills: s?.kills ?? null,
          deaths: s?.deaths ?? null,
          assists: s?.assists ?? null,
          adr: s?.adr ?? null,
          hs_pct: s?.hs_pct ?? null,
        }
      }),
    rounds: [...file.rounds]
      .sort((a, b) => a.n - b.n)
      .map((r) => ({ n: r.n, winner: r.winner, reason: r.reason, side_a: r.side_a })),
  }
}

export function roundFileName(n: number): string {
  return `${String(n).padStart(2, '0')}.json`
}
