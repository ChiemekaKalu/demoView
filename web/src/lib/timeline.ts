// Time, frames and ticks for a round, and the round's kills on that timeline.
//
// Frame i of a round file is tick start_tick + i * tick_rate / hz, and
// playback time t (seconds) is i / hz, so t = 0 is the end of freeze time.

import { clamp } from './interp'
import type { MatchFile, RoundFile } from './types'

export function roundDuration(round: Pick<RoundFile, 'frames' | 'hz'>): number {
  return Math.max(round.frames - 1, 0) / round.hz
}

// Fractional frame at time t, clamped to the round.
export function timeToFrame(t: number, round: Pick<RoundFile, 'frames' | 'hz'>): number {
  return clamp(t * round.hz, 0, Math.max(round.frames - 1, 0))
}

// A tick's frame: the first frame at or after the tick (schema convention).
export function tickToFrame(tick: number, startTick: number, tickRate: number, hz: number): number {
  return Math.ceil(((tick - startTick) * hz) / tickRate)
}

export interface FeedKill {
  frame: number
  // Index into match.json kills, or null when the kill is known only from
  // the round file.
  kill: number | null
  attacker: number | null
  victim: number | null
  assister: number | null
  weapon: string | null
  headshot: boolean
}

// The round's kills in frame order.
//
// With match.json: each `kill` event gives its frame and its index into
// match.json kills. Kills of this round that no event references get their
// frame from their tick.
//
// Without match.json, the round file still says when kills happen, and the victim is the player whose hp first hits 0
// on that frame. The attacker and weapon are unknown.
export function roundKills(round: RoundFile, match: MatchFile | null): FeedKill[] {
  const last = Math.max(round.frames - 1, 0)
  const out: FeedKill[] = []
  if (match) {
    const seen = new Set<number>()
    for (const e of round.events) {
      if (e.type !== 'kill' || e.kill === undefined) continue
      const k = match.kills[e.kill]
      if (!k || seen.has(e.kill)) continue
      seen.add(e.kill)
      out.push({ frame: clamp(e.frame, 0, last), kill: e.kill, ...pickKill(k) })
    }
    match.kills.forEach((k, i) => {
      if (k.round !== round.round || seen.has(i)) return
      const frame = tickToFrame(k.tick, round.start_tick, match.match.tick_rate, round.hz)
      out.push({ frame: clamp(frame, 0, last), kill: i, ...pickKill(k) })
    })
  } else {
    const claimed = new Set<number>()
    for (const e of round.events) {
      if (e.type !== 'kill') continue
      const victim = round.players.find(
        (p) => !claimed.has(p.idx) && p.hp[e.frame] === 0 && (e.frame === 0 || (p.hp[e.frame - 1] ?? 0) > 0),
      )
      if (victim) claimed.add(victim.idx)
      out.push({
        frame: clamp(e.frame, 0, last),
        kill: e.kill ?? null,
        attacker: null,
        victim: victim?.idx ?? null,
        assister: null,
        weapon: null,
        headshot: false,
      })
    }
  }
  return out.sort((a, b) => a.frame - b.frame || (a.kill ?? 0) - (b.kill ?? 0))
}

function pickKill(k: MatchFile['kills'][number]) {
  return { attacker: k.attacker, victim: k.victim, assister: k.assister, weapon: k.weapon, headshot: k.headshot }
}

// "1:05" for 65 seconds.
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

// Reads `?t=` (seconds). Bad or missing values give 0; the result is clamped
// to [0, max].
export function parseT(value: string | null, max: number): number {
  if (value === null || value.trim() === '') return 0
  const t = Number(value)
  return Number.isFinite(t) ? clamp(t, 0, max) : 0
}

// Seconds for `?t=`, to a tenth, without a trailing ".0".
export function formatT(seconds: number): string {
  return String(Math.round(Math.max(0, seconds) * 10) / 10)
}

// The current URL with `t` set (or removed at 0), for history.replaceState.
export function urlWithT(href: string, seconds: number): string {
  const url = new URL(href)
  if (seconds > 0) url.searchParams.set('t', formatT(seconds))
  else url.searchParams.delete('t')
  return url.pathname + url.search + url.hash
}
