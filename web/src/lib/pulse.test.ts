import { describe, expect, it } from 'vitest'
import { oddsAt, percent, pulseLine, type PulseRound, swingAt } from './pulse'

const round: PulseRound = {
  n: 3,
  moments: [
    { tick: 1000, p_ct: 0.5, p_raw: 0.5, tension: 2 },
    { tick: 1640, p_ct: 0.3, p_raw: 0.38, tension: 1.8 },
    { tick: 2280, p_ct: 0.85, p_raw: 0.7, tension: 1 },
  ],
  swing_tick: 2280,
  highlight: 2,
  share: 0.4,
  headline: null,
}

describe('pulseLine', () => {
  it('puts each moment in replay seconds from the round file start', () => {
    expect(pulseLine(round, 1000, 64)).toEqual([
      { t: 0, p: 0.5 },
      { t: 10, p: 0.3 },
      { t: 20, p: 0.85 },
    ])
  })
})

describe('oddsAt', () => {
  const line = pulseLine(round, 1000, 64)

  it('holds each moment until the next', () => {
    expect(oddsAt(line, 0)).toBe(0.5)
    expect(oddsAt(line, 9.9)).toBe(0.5)
    expect(oddsAt(line, 10)).toBe(0.3)
    expect(oddsAt(line, 99)).toBe(0.85)
  })

  it('has nothing before the first moment', () => {
    expect(oddsAt(pulseLine(round, 0, 64), 1)).toBeNull()
  })
})

describe('swingAt', () => {
  it('gives the odds before and after a kill', () => {
    expect(swingAt(round, 2280)).toEqual({ before: 0.3, after: 0.85 })
  })

  it('has no swing for the first moment or a tick without one', () => {
    expect(swingAt(round, 1000)).toBeNull()
    expect(swingAt(round, 1234)).toBeNull()
  })
})

describe('percent', () => {
  it('rounds to whole percents', () => {
    expect(percent(0.416)).toBe('42%')
  })
})
