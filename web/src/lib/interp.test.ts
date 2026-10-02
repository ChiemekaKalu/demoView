import { describe, expect, it } from 'vitest'
import { lerpAngle, playerAt, sampleAt, wrapDeg, yawToCanvas } from './interp'
import type { PlayerTrack } from './types'

describe('lerpAngle', () => {
  it('interpolates normally inside the circle', () => {
    expect(lerpAngle(10, 30, 0.5)).toBeCloseTo(20)
    expect(lerpAngle(90, 180, 0.25)).toBeCloseTo(112.5)
  })

  it('wraps 359 -> 1 through 0, not around the circle', () => {
    expect(lerpAngle(359, 1, 0.5)).toBeCloseTo(0)
    expect(lerpAngle(359, 1, 0.25)).toBeCloseTo(359.5)
    expect(lerpAngle(359, 1, 0.75)).toBeCloseTo(0.5)
  })

  it('wraps 1 -> 359 backwards through 0', () => {
    expect(lerpAngle(1, 359, 0.5)).toBeCloseTo(0)
    expect(lerpAngle(10, 350, 0.5)).toBeCloseTo(0)
  })

  it('takes the short way across 180', () => {
    expect(lerpAngle(170, 190, 0.5)).toBeCloseTo(180)
    expect(lerpAngle(300, 60, 0.5)).toBeCloseTo(0)
  })

  it('returns the endpoints at t = 0 and t = 1, in [0, 360)', () => {
    expect(lerpAngle(359, 1, 0)).toBeCloseTo(359)
    expect(lerpAngle(359, 1, 1)).toBeCloseTo(1)
    for (let t = 0; t <= 1; t += 0.1) {
      const a = lerpAngle(350, 20, t)
      expect(a).toBeGreaterThanOrEqual(0)
      expect(a).toBeLessThan(360)
    }
  })
})

describe('wrapDeg', () => {
  it('wraps into [0, 360)', () => {
    expect(wrapDeg(360)).toBe(0)
    expect(wrapDeg(-90)).toBe(270)
    expect(wrapDeg(725)).toBe(5)
  })
})

describe('sampleAt', () => {
  const col = [0, 10, 30]
  it('interpolates between frames', () => {
    expect(sampleAt(col, 0.5)).toBe(5)
    expect(sampleAt(col, 1.25)).toBe(15)
  })
  it('clamps outside the column', () => {
    expect(sampleAt(col, -3)).toBe(0)
    expect(sampleAt(col, 2)).toBe(30)
    expect(sampleAt(col, 99)).toBe(30)
  })
})

describe('playerAt', () => {
  const track: PlayerTrack = {
    idx: 4,
    x: [0, 16, 32, 32, 32],
    y: [100, 100, 80, 80, 80],
    yaw: [350, 10, 20, 20, 20],
    hp: [100, 100, 40, 0, 0],
  }

  it('interpolates position and yaw between frames', () => {
    const s = playerAt(track, 0.5)
    expect(s.x).toBe(8)
    expect(s.y).toBe(100)
    expect(s.yaw).toBeCloseTo(0)
    expect(s.alive).toBe(true)
  })

  it("uses the earlier frame's health", () => {
    expect(playerAt(track, 1.9).hp).toBe(100)
    expect(playerAt(track, 2).hp).toBe(40)
  })

  it('is alive just before the death frame and dead on it', () => {
    expect(playerAt(track, 2.99).alive).toBe(true)
    const dead = playerAt(track, 3)
    expect(dead.alive).toBe(false)
    expect([dead.x, dead.y]).toEqual([32, 80])
  })

  it('holds the death spot after death', () => {
    const s = playerAt(track, 3.6)
    expect([s.x, s.y, s.hp, s.alive]).toEqual([32, 80, 0, false])
  })

  it('clamps past either end', () => {
    expect(playerAt(track, -1).x).toBe(0)
    expect(playerAt(track, 50).x).toBe(32)
  })
})

describe('yawToCanvas', () => {
  // On the canvas, +x is right and +y is down.
  const dir = (yaw: number) => {
    const a = yawToCanvas(yaw)
    // `+ 0` turns -0 into 0.
    return [Math.round(Math.cos(a)) + 0, Math.round(Math.sin(a)) + 0]
  }
  it('points yaw 0 right', () => expect(dir(0)).toEqual([1, 0]))
  it('points yaw 90 up (negative canvas y)', () => expect(dir(90)).toEqual([0, -1]))
  it('points yaw 180 left', () => expect(dir(180)).toEqual([-1, 0]))
  it('points yaw 270 down', () => expect(dir(270)).toEqual([0, 1]))
})
