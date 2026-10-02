// The playback clock: a position in seconds, a play state and a speed. It
// knows nothing about frames or the DOM; the stage advances it once per
// animation frame.

import { clamp } from './interp'

export const SPEEDS = [0.5, 1, 2, 4] as const

export class Clock {
  t = 0
  playing = false
  speed = 1
  duration: number

  constructor(duration: number, t = 0) {
    this.duration = Math.max(0, duration)
    this.t = clamp(t, 0, this.duration)
  }

  play(): void {
    // Playing from the end starts over.
    if (this.t >= this.duration) this.t = 0
    this.playing = this.duration > 0
  }

  pause(): void {
    this.playing = false
  }

  toggle(): void {
    if (this.playing) this.pause()
    else this.play()
  }

  seek(t: number): void {
    this.t = clamp(t, 0, this.duration)
  }

  step(seconds: number): void {
    this.seek(this.t + seconds)
  }

  // Moves the clock by `dt` seconds of wall time. Returns true when this call
  // reached the end, which also pauses.
  advance(dt: number): boolean {
    if (!this.playing || !(dt > 0)) return false
    this.t += dt * this.speed
    if (this.t >= this.duration) {
      this.t = this.duration
      this.playing = false
      return true
    }
    return false
  }
}
