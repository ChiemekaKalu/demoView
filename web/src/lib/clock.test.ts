import { describe, expect, it } from 'vitest'
import { Clock } from './clock'

describe('Clock', () => {
  it('starts paused at the given time, clamped', () => {
    expect(new Clock(90, 42).t).toBe(42)
    expect(new Clock(90, 500).t).toBe(90)
    expect(new Clock(90).playing).toBe(false)
  })

  it('advances only while playing, scaled by speed', () => {
    const c = new Clock(90)
    c.advance(1)
    expect(c.t).toBe(0)
    c.play()
    c.advance(1)
    expect(c.t).toBe(1)
    c.speed = 4
    c.advance(0.5)
    expect(c.t).toBe(3)
    c.speed = 0.5
    c.advance(2)
    expect(c.t).toBe(4)
  })

  it('stops at the end and reports it once', () => {
    const c = new Clock(10, 9)
    c.play()
    expect(c.advance(5)).toBe(true)
    expect(c.t).toBe(10)
    expect(c.playing).toBe(false)
    expect(c.advance(1)).toBe(false)
  })

  it('starts over when played from the end', () => {
    const c = new Clock(10, 10)
    c.toggle()
    expect(c.playing).toBe(true)
    expect(c.t).toBe(0)
  })

  it('steps and seeks within the round', () => {
    const c = new Clock(10, 3)
    c.step(5)
    expect(c.t).toBe(8)
    c.step(5)
    expect(c.t).toBe(10)
    c.step(-50)
    expect(c.t).toBe(0)
  })
})
