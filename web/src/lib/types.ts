// Shapes of the data the viewer reads. The files follow schema/*.schema.json
// (schema_version 1); the summary follows GET /api/matches/:id in
// worker/README.md.

export type Team = 'A' | 'B'
export type Side = 'CT' | 'T'

export interface MatchFile {
  schema_version: number
  parser_version: string
  match: { map: string; tick_rate: number; demo_sha256: string }
  players: { idx: number; steam_id: string; name: string; team: Team }[]
  rounds: MatchRound[]
  kills: Kill[]
  scoreboard: ScoreRow[]
}

export interface MatchRound {
  n: number
  start_tick: number
  freeze_end_tick: number
  end_tick: number
  winner: Team
  reason: string
  side_a: Side
}

export interface Kill {
  round: number
  tick: number
  attacker: number | null
  victim: number
  assister: number | null
  weapon: string
  headshot: boolean
}

export interface ScoreRow {
  player: number
  kills: number
  deaths: number
  assists: number
  adr: number
  hs_pct: number
}

export interface RoundFile {
  schema_version: number
  round: number
  hz: number
  start_tick: number
  frames: number
  players: PlayerTrack[]
  grenades: Grenade[]
  events: RoundEvent[]
}

export interface PlayerTrack {
  idx: number
  x: number[]
  y: number[]
  yaw: number[]
  hp: number[]
}

export interface Grenade {
  type: string
  thrower: number
  throw_frame: number
  detonate_frame: number
  end_frame: number
  path: [number, number][]
}

export interface RoundEvent {
  frame: number
  type: string
  kill?: number
}

export type MatchStatus = 'uploaded' | 'queued' | 'parsing' | 'ready' | 'failed'

// GET /api/matches/:id. D1 columns can be null until a parse fills them.
export interface MatchSummary {
  id: string
  status: MatchStatus
  error: string | null
  map: string | null
  score_a: number | null
  score_b: number | null
  parser_version: string | null
  schema_version: number | null
  created_at: number
  parsed_at: number | null
  players: SummaryPlayer[]
  rounds: SummaryRound[]
}

export interface SummaryPlayer {
  idx: number
  name: string | null
  team: string | null
  kills: number | null
  deaths: number | null
  assists: number | null
  adr: number | null
  hs_pct: number | null
}

export interface SummaryRound {
  n: number
  winner: string | null
  reason: string | null
  side_a: string | null
}
