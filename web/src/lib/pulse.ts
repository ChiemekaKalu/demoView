// Match Pulse (schema/pulse.schema.json): Jev's calibrated chance that the CTs
// win, at round start and after every kill. The viewer only draws it.

export const FALLBACK_MODEL = 'fallback-logit-v1'

export interface PulseMoment {
  tick: number
  p_ct: number
  p_raw: number
  tension: number
}

export interface PulseRound {
  n: number
  moments: PulseMoment[]
  swing_tick: number | null
  highlight: number
  share: number
  headline: string | null
}

export interface PulseFile {
  pulse_version: number
  model: string
  rounds: PulseRound[]
  worth_watching: number[]
}

export interface PulsePoint {
  t: number // seconds into the replay
  p: number // chance the CTs win
}

// The round's line in replay time: each moment's odds hold until the next.
// Frame 0 of a round file is its start_tick.
export function pulseLine(round: PulseRound, startTick: number, tickRate: number): PulsePoint[] {
  return round.moments.map((m) => ({ t: Math.max(0, (m.tick - startTick) / tickRate), p: m.p_ct }))
}

// The odds at time t: those of the last moment at or before it.
export function oddsAt(line: readonly PulsePoint[], t: number): number | null {
  let p: number | null = null
  for (const pt of line) {
    if (pt.t > t) break
    p = pt.p
  }
  return p
}

// How a kill at tick moved the odds, or null when no moment sits on it.
export function swingAt(round: PulseRound, tick: number): { before: number; after: number } | null {
  const i = round.moments.findIndex((m) => m.tick === tick)
  if (i < 1) return null
  return { before: round.moments[i - 1]!.p_ct, after: round.moments[i]!.p_ct }
}

export function percent(p: number): string {
  return `${Math.round(p * 100)}%`
}
