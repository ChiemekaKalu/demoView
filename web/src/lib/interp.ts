// Interpolation between 16 Hz (or whatever `hz` is) frames, so the canvas can
// draw at the display's refresh rate.

import type { PlayerTrack } from './types'

export function clamp(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v
}

export function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t
}

// Wraps degrees into [0, 360).
export function wrapDeg(d: number): number {
  const r = d % 360
  return r < 0 ? r + 360 : r
}

// Interpolates two angles in degrees along the shorter arc, so 359 -> 1 goes
// through 0 rather than all the way around. The result is in [0, 360).
export function lerpAngle(a: number, b: number, t: number): number {
  let diff = wrapDeg(b - a)
  if (diff > 180) diff -= 360
  return wrapDeg(a + diff * t)
}

// Linear interpolation into a column at a fractional frame, clamped to the
// column's ends.
export function sampleAt(col: readonly number[], f: number): number {
  const n = col.length
  if (n === 0) return 0
  if (!(f > 0)) return col[0]
  if (f >= n - 1) return col[n - 1]
  const i = Math.floor(f)
  return lerp(col[i], col[i + 1], f - i)
}

export interface PlayerState {
  idx: number
  x: number
  y: number
  yaw: number
  hp: number
  alive: boolean
}

// A player's state at fractional frame `f`. Health is not interpolated: it
// is the value of the frame at or before `f`. A dead player holds the
// position and yaw they died with (the schema keeps them in the columns).
export function playerAt(track: PlayerTrack, f: number): PlayerState {
  const n = track.hp.length
  const i = clamp(Math.floor(f), 0, Math.max(n - 1, 0))
  const j = Math.min(i + 1, n - 1)
  const t = clamp(f - i, 0, 1)
  const hp = track.hp[i] ?? 0
  if (hp <= 0 || j === i) {
    return { idx: track.idx, x: track.x[i], y: track.y[i], yaw: wrapDeg(track.yaw[i]), hp, alive: hp > 0 }
  }
  return {
    idx: track.idx,
    x: lerp(track.x[i], track.x[j], t),
    y: lerp(track.y[i], track.y[j], t),
    yaw: lerpAngle(track.yaw[i], track.yaw[j], t),
    hp,
    alive: true,
  }
}

// Game yaw is degrees counterclockwise from +x with y up. Canvas y points
// down, so the same direction on the canvas is the negated angle (radians,
// clockwise-positive, as CanvasRenderingContext2D.arc expects).
export function yawToCanvas(yawDeg: number): number {
  return (-yawDeg * Math.PI) / 180
}
