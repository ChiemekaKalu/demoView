import { describe, expect, it } from 'vitest'
import matchJson from '../../../schema/samples/match.json'
import round01Json from '../../../schema/samples/rounds/01.json'
import round16Json from '../../../schema/samples/rounds/16.json'
import {
  formatClock,
  formatT,
  parseT,
  roundDuration,
  roundKills,
  tickToFrame,
  timeToFrame,
  urlWithT,
} from './timeline'
import type { MatchFile, RoundFile } from './types'

const match = matchJson as unknown as MatchFile
const round01 = round01Json as unknown as RoundFile
const round16 = round16Json as unknown as RoundFile

describe('time and frames', () => {
  const round = { frames: 1456, hz: 16 }

  it('reads hz from the round instead of assuming 16', () => {
    expect(timeToFrame(2, { frames: 1000, hz: 32 })).toBe(64)
    expect(roundDuration({ frames: 321, hz: 32 })).toBe(10)
  })

  it('has the last frame at the end of the round', () => {
    expect(roundDuration(round)).toBeCloseTo(90.9375)
    expect(timeToFrame(roundDuration(round), round)).toBe(1455)
  })

  it('maps time to frames', () => {
    expect(timeToFrame(42, round)).toBe(672)
    expect(timeToFrame(0.03125, round)).toBe(0.5)
  })

  it('clamps time to the round', () => {
    expect(timeToFrame(-5, round)).toBe(0)
    expect(timeToFrame(9999, round)).toBe(1455)
  })

  it('converts ticks with the schema convention (ceil)', () => {
    // 64 tick, 16 Hz: 4 ticks per frame.
    expect(tickToFrame(1000, 1000, 64, 16)).toBe(0)
    expect(tickToFrame(1001, 1000, 64, 16)).toBe(1)
    expect(tickToFrame(1004, 1000, 64, 16)).toBe(1)
    expect(tickToFrame(1005, 1000, 64, 16)).toBe(2)
  })
})

describe('roundKills', () => {
  it("maps round 1's kill events to match.json kills", () => {
    const events = round01.events.filter((e) => e.type === 'kill')
    const feed = roundKills(round01, match)
    expect(feed).toHaveLength(events.length)
    const { attacker, victim, assister, weapon, headshot } = match.kills[events[0].kill!]
    expect(feed[0]).toEqual({ frame: events[0].frame, kill: events[0].kill, attacker, victim, assister, weapon, headshot })
    for (let i = 1; i < feed.length; i++) expect(feed[i].frame).toBeGreaterThanOrEqual(feed[i - 1].frame)
  })

  it('puts every kill on the frame its tick maps to, where the victim first has 0 hp', () => {
    for (const round of [round01, round16]) {
      for (const k of roundKills(round, match)) {
        const tick = match.kills[k.kill!].tick
        expect(k.frame).toBe(tickToFrame(tick, round.start_tick, match.match.tick_rate, round.hz))
        const victim = round.players.find((p) => p.idx === k.victim)!
        expect(victim.hp[k.frame]).toBe(0)
        expect(victim.hp[k.frame - 1]).toBeGreaterThan(0)
      }
    }
  })

  it('derives frames from ticks for kills no event references', () => {
    const noEvents = { ...round01, events: [] }
    const feed = roundKills(noEvents, match)
    expect(feed.map((k) => k.frame)).toEqual(roundKills(round01, match).map((k) => k.frame))
  })

  it('without match.json, infers victims from hp', () => {
    const feed = roundKills(round01, null)
    const full = roundKills(round01, match)
    expect(feed.map((k) => k.frame)).toEqual(full.map((k) => k.frame))
    expect(feed.map((k) => k.victim)).toEqual(full.map((k) => k.victim))
    expect(feed.every((k) => k.attacker === null && k.weapon === null)).toBe(true)
  })
})

describe('?t= deep links', () => {
  it('parses seconds and clamps them', () => {
    expect(parseT('42', 90)).toBe(42)
    expect(parseT('42.5', 90)).toBe(42.5)
    expect(parseT('500', 90)).toBe(90)
    expect(parseT('-3', 90)).toBe(0)
  })

  it('treats missing or bad values as 0', () => {
    expect(parseT(null, 90)).toBe(0)
    expect(parseT('', 90)).toBe(0)
    expect(parseT('abc', 90)).toBe(0)
  })

  it('formats to a tenth of a second', () => {
    expect(formatT(42)).toBe('42')
    expect(formatT(42.26)).toBe('42.3')
    expect(formatT(0.04)).toBe('0')
  })

  it('updates only t in the URL', () => {
    expect(urlWithT('https://x.dev/m/abc/r/3?foo=1', 12.5)).toBe('/m/abc/r/3?foo=1&t=12.5')
    expect(urlWithT('https://x.dev/m/abc/r/3?t=9', 42)).toBe('/m/abc/r/3?t=42')
    expect(urlWithT('https://x.dev/m/abc/r/3?t=9', 0)).toBe('/m/abc/r/3')
  })

  it('formats a clock', () => {
    expect(formatClock(0)).toBe('0:00')
    expect(formatClock(65.9)).toBe('1:05')
    expect(formatClock(600)).toBe('10:00')
  })
})
