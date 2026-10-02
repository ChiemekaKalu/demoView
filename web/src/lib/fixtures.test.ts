import { describe, expect, it } from 'vitest'
import matchJson from '../../../schema/samples/match.json'
import { deriveSummary, roundFileName } from './fixtures'
import type { MatchFile } from './types'

const match = matchJson as unknown as MatchFile

describe('deriveSummary', () => {
  const summary = deriveSummary(match, 'sample')

  it('has exactly the GET /api/matches/:id keys', () => {
    expect(Object.keys(summary).sort()).toEqual(
      [
        'id',
        'status',
        'error',
        'map',
        'score_a',
        'score_b',
        'parser_version',
        'schema_version',
        'created_at',
        'parsed_at',
        'players',
        'rounds',
      ].sort(),
    )
    expect(summary).toMatchObject({ id: 'sample', status: 'ready', error: null, map: 'de_mirage', schema_version: 1 })
  })

  it('counts the final score from round winners (13-9)', () => {
    expect([summary.score_a, summary.score_b]).toEqual([13, 9])
  })

  it('merges each player with their scoreboard row and drops steam_id', () => {
    expect(summary.players).toHaveLength(10)
    expect(Object.keys(summary.players[0]).sort()).toEqual(
      ['adr', 'assists', 'deaths', 'hs_pct', 'idx', 'kills', 'name', 'team'].sort(),
    )
    expect(summary.players[0]).toEqual({
      idx: 0,
      name: 'Ashgrove',
      team: 'A',
      kills: 17,
      deaths: 9,
      assists: 4,
      adr: 81.9,
      hs_pct: 70.6,
    })
  })

  it('keeps only n, winner, reason and side_a per round, ordered by n', () => {
    expect(summary.rounds).toHaveLength(22)
    expect(summary.rounds[0]).toEqual({ n: 1, winner: 'A', reason: 'bomb_defused', side_a: 'CT' })
    expect(summary.rounds.map((r) => r.n)).toEqual(Array.from({ length: 22 }, (_, i) => i + 1))
  })

  it('fills missing scoreboard rows with null, like the D1 columns', () => {
    const partial = { ...match, scoreboard: match.scoreboard.slice(1) }
    expect(deriveSummary(partial, 'x').players[0]).toMatchObject({ idx: 0, kills: null, adr: null })
  })
})

describe('roundFileName', () => {
  it('zero-pads to two digits', () => {
    expect(roundFileName(1)).toBe('01.json')
    expect(roundFileName(21)).toBe('21.json')
    expect(roundFileName(100)).toBe('100.json')
  })
})
