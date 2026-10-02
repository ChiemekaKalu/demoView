// Owns the canvas: sizing, the animation loop and the playback clock. It
// draws on requestAnimationFrame only while playing or after a change, and
// interpolates between data frames, so motion is smooth at any refresh rate.

import { Clock } from '../lib/clock'
import { Renderer, type Scene } from './draw'

export type UpdateFn = (t: number, playing: boolean, ended: boolean) => void

// The longest wall-clock step one animation frame may advance, so a
// backgrounded tab doesn't jump ahead when it comes back.
const MAX_STEP = 0.25

export class Stage {
  readonly clock: Clock
  onUpdate: UpdateFn = () => {}

  private canvas: HTMLCanvasElement
  private ctx: CanvasRenderingContext2D
  private renderer: Renderer
  private hz: number
  private raf = 0
  private last = 0
  private size = 0
  private ratio = 1
  private observer: ResizeObserver

  constructor(canvas: HTMLCanvasElement, scene: Scene, duration: number, t: number) {
    this.canvas = canvas
    this.ctx = canvas.getContext('2d', { alpha: false })!
    this.renderer = new Renderer(scene)
    this.hz = scene.round.hz
    this.clock = new Clock(duration, t)
    this.observer = new ResizeObserver(([entry]) => this.resize(entry))
    try {
      // Also fires when the device pixel ratio changes (zoom, moving screens).
      this.observer.observe(canvas, { box: 'device-pixel-content-box' })
    } catch {
      this.observer.observe(canvas)
    }
  }

  destroy(): void {
    cancelAnimationFrame(this.raf)
    this.raf = 0
    this.observer.disconnect()
  }

  setRadar(img: HTMLImageElement | null): void {
    this.renderer.setRadar(img)
    this.request()
  }

  toggle(): void {
    this.clock.toggle()
    this.last = 0
    this.request()
  }

  pause(): void {
    this.clock.pause()
    this.request()
  }

  seek(t: number): void {
    this.clock.seek(t)
    this.request()
  }

  step(seconds: number): void {
    this.clock.step(seconds)
    this.request()
  }

  setSpeed(speed: number): void {
    this.clock.speed = speed
    this.request()
  }

  private resize(entry: ResizeObserverEntry): void {
    const css = entry.contentBoxSize?.[0]?.inlineSize ?? entry.contentRect.width
    if (!(css > 0)) return
    // The exact device-pixel size when the browser reports one, unless it
    // disagrees with the pixel ratio (seen in emulated devices).
    const expected = css * devicePixelRatio
    const reported = entry.devicePixelContentBoxSize?.[0]?.inlineSize
    const device = reported && Math.abs(reported - expected) <= 2 ? reported : Math.round(expected)
    if (this.canvas.width !== device) this.canvas.width = this.canvas.height = device
    this.size = css
    this.ratio = device / css
    this.request()
  }

  private request(): void {
    if (!this.raf) this.raf = requestAnimationFrame(this.frame)
  }

  private frame = (now: number): void => {
    this.raf = 0
    let ended = false
    if (this.clock.playing) {
      if (this.last) ended = this.clock.advance(Math.min((now - this.last) / 1000, MAX_STEP))
      this.last = now
    } else {
      this.last = 0
    }
    if (this.size > 0) this.renderer.draw(this.ctx, this.clock.t * this.hz, this.size, this.ratio)
    this.onUpdate(this.clock.t, this.clock.playing, ended)
    if (this.clock.playing) this.request()
  }
}

// Radar images by URL, loaded once. Resolves to null when there is none.
const radars = new Map<string, Promise<HTMLImageElement | null>>()

export function loadRadar(url: string): Promise<HTMLImageElement | null> {
  let p = radars.get(url)
  if (!p) {
    p = new Promise((resolve) => {
      const img = new Image()
      img.decoding = 'async'
      img.onload = () => resolve(img.naturalWidth > 0 ? img : null)
      img.onerror = () => resolve(null)
      img.src = url
    })
    radars.set(url, p)
  }
  return p
}
