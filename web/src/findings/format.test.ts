import { describe, expect, it } from 'vitest'
import data from './data.json'
import { interval, mapTitle, pct, type Rate, tip, usd } from './format'

describe('format', () => {
  const r: Rate = { n: 1707, p: 0.574, lo: 0.551, hi: 0.597 }

  it('rounds rates to whole percents', () => {
    expect(pct(0.574)).toBe('57%')
    expect(interval(r)).toBe('55–60%')
    expect(tip(r, 'rounds')).toBe('57% (95% interval 55–60%), 1,707 rounds')
  })

  it('names maps as the game does', () => {
    expect(mapTitle('dust2')).toBe('Dust2')
    expect(mapTitle('nuke')).toBe('Nuke')
  })

  it('prints per-call costs at a readable precision', () => {
    expect(usd(0.0028)).toBe('$0.0028')
    expect(usd(0.000029)).toBe('$0.00003')
    expect(usd(0)).toBe('free')
    expect(usd(null)).toBe('—')
  })
})

// Every object with n, p, lo and hi in the data file.
function rates(v: unknown): Rate[] {
  if (Array.isArray(v)) return v.flatMap(rates)
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>
    const own = ['n', 'p', 'lo', 'hi'].every((k) => typeof o[k] === 'number') ? [o as unknown as Rate] : []
    return [...own, ...Object.values(o).flatMap(rates)]
  }
  return []
}

describe('data.json', () => {
  it('holds rates inside their intervals', () => {
    const all = rates(data)
    expect(all.length).toBeGreaterThan(50)
    for (const r of all) {
      expect(r.lo).toBeLessThanOrEqual(r.p)
      expect(r.p).toBeLessThanOrEqual(r.hi)
      expect(r.n).toBeGreaterThan(0)
    }
  })

  it('sorts maps from most CT-sided to most T-sided', () => {
    const ps = data.maps.map((m) => m.p)
    expect(ps).toEqual([...ps].sort((a, b) => b - a))
  })

  it('keeps the plant staircase rising with the T head count', () => {
    const steps = data.post_plant.by_alive_diff
    expect(steps.map((s) => s.diff)).toEqual([-3, -2, -1, 0, 1, 2, 3])
    for (let i = 1; i < steps.length; i++) expect(steps[i].p).toBeGreaterThan(steps[i - 1].p)
  })

  it('carries no player or team names, only callouts', () => {
    const text = JSON.stringify(data)
    expect(text).not.toMatch(/7656119\d{10}|demos\/|\.dem\b/)
  })
})
