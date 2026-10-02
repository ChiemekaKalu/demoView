// Draws one moment of a round onto a canvas. All sizes are CSS pixels; the
// caller sets the canvas backing store to CSS size x device pixel ratio.

import { clamp, playerAt, yawToCanvas, type PlayerState } from '../lib/interp'
import { placeLabels, type LabelSpot } from '../lib/labels'
import { radarToWorld, worldToRadar, type MapInfo } from '../lib/maps'
import type { FeedKill } from '../lib/timeline'
import type { Grenade, RoundFile, Side } from '../lib/types'

export const SIDE_COLOR: Record<Side, string> = { CT: '#5ab0ff', T: '#f5b83d' }
const SIDE_FILL: Record<Side, string> = { CT: 'rgba(90,176,255,0.36)', T: 'rgba(245,184,61,0.36)' }

const GRENADE_COLOR: Record<string, string> = {
  smoke: '#cfd5de',
  flash: '#fff6b8',
  he: '#ff6a5c',
  molotov: '#ff8a2a',
  incendiary: '#ff8a2a',
  decoy: '#a6dc9f',
}

// Effect sizes in game units.
const SMOKE_RADIUS = 144
const FIRE_RADIUS = 120
const FLASH_RADIUS = 200
const HE_RADIUS = 260
const PULSE_SECONDS = 0.6
const TRAIL_FADE_SECONDS = 1
const KILL_RING_SECONDS = 0.8

export interface Scene {
  round: RoundFile
  map: MapInfo
  names: ReadonlyMap<number, string>
  sides: ReadonlyMap<number, Side>
  kills: readonly FeedKill[]
}

export class Renderer {
  private scene: Scene
  private radar: HTMLImageElement | null = null
  private bg: HTMLCanvasElement | null = null
  private bgKey = ''

  constructor(scene: Scene) {
    this.scene = scene
  }

  setRadar(img: HTMLImageElement | null): void {
    this.radar = img
    this.bg = null
  }

  // f is a fractional frame.
  draw(ctx: CanvasRenderingContext2D, f: number, size: number, ratio: number): void {
    const { round, map, kills } = this.scene
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0)
    ctx.drawImage(this.background(size, ratio), 0, 0, size, size)

    const k = size / map.size
    const unit = k / map.scale // CSS pixels per game unit
    const pt = (x: number, y: number): [number, number] => {
      const [px, py] = worldToRadar(map, x, y)
      return [px * k, py * k]
    }
    const r = clamp(size / 110, 4, 8)
    const hz = round.hz

    // Areas under everything, then pulses and flight paths.
    for (const g of round.grenades) drawArea(ctx, g, f, hz, pt, unit)
    for (const g of round.grenades) drawPulse(ctx, g, f, hz, pt, unit)
    for (const g of round.grenades) drawTrail(ctx, g, f, hz, pt)

    const states = round.players.map((p) => playerAt(p, f))
    for (const s of states) if (!s.alive) this.drawDead(ctx, s, pt, r)
    for (const s of states) if (s.alive) this.drawWedge(ctx, s, pt, r)
    for (const s of states) if (s.alive) this.drawDot(ctx, s, pt, r)
    const fontPx = Math.round(clamp(size / 62, 10, 13))
    ctx.font = `600 ${fontPx}px system-ui, sans-serif`
    ctx.textBaseline = 'middle'
    ctx.lineJoin = 'round'
    const living = states.filter((s) => s.alive)
    const names = living.map((s) => shorten(this.scene.names.get(s.idx) ?? `#${s.idx}`))
    const spots = placeLabels(
      living.map((s, i) => {
        const [x, y] = pt(s.x, s.y)
        return { x, y, width: ctx.measureText(names[i]).width }
      }),
      r,
      fontPx,
    )
    spots.forEach((spot, i) => drawLabel(ctx, names[i], spot))

    // A ring where someone just died.
    for (const kill of kills) {
      const p = (f - kill.frame) / (KILL_RING_SECONDS * hz)
      if (p < 0 || p >= 1 || kill.victim === null) continue
      const v = states.find((s) => s.idx === kill.victim)
      if (!v) continue
      const [x, y] = pt(v.x, v.y)
      ctx.strokeStyle = `rgba(255,77,77,${1 - p})`
      ctx.lineWidth = 2
      ctx.beginPath()
      ctx.arc(x, y, r * (1.4 + 2.2 * p), 0, Math.PI * 2)
      ctx.stroke()
    }
  }

  private side(idx: number): Side {
    return this.scene.sides.get(idx) ?? 'CT'
  }

  private drawDead(ctx: CanvasRenderingContext2D, s: PlayerState, pt: Pt, r: number) {
    const [x, y] = pt(s.x, s.y)
    const d = r * 0.85
    const cross = () => {
      ctx.beginPath()
      ctx.moveTo(x - d, y - d)
      ctx.lineTo(x + d, y + d)
      ctx.moveTo(x + d, y - d)
      ctx.lineTo(x - d, y + d)
      ctx.stroke()
    }
    ctx.lineCap = 'round'
    ctx.strokeStyle = 'rgba(0,0,0,0.65)'
    ctx.lineWidth = 4.5
    cross()
    ctx.strokeStyle = SIDE_COLOR[this.side(s.idx)]
    ctx.lineWidth = 2.25
    cross()
  }

  private drawWedge(ctx: CanvasRenderingContext2D, s: PlayerState, pt: Pt, r: number) {
    const [x, y] = pt(s.x, s.y)
    const a = yawToCanvas(s.yaw)
    ctx.fillStyle = SIDE_FILL[this.side(s.idx)]
    ctx.beginPath()
    ctx.moveTo(x, y)
    ctx.arc(x, y, r * 3.6, a - 0.42, a + 0.42)
    ctx.closePath()
    ctx.fill()
  }

  // CT is a circle and T a square, so sides differ by shape as well as color.
  private drawDot(ctx: CanvasRenderingContext2D, s: PlayerState, pt: Pt, r: number) {
    const [x, y] = pt(s.x, s.y)
    const side = this.side(s.idx)
    ctx.fillStyle = SIDE_COLOR[side]
    ctx.strokeStyle = '#0b0d10'
    ctx.lineWidth = 1.5
    ctx.beginPath()
    if (side === 'CT') ctx.arc(x, y, r, 0, Math.PI * 2)
    else ctx.rect(x - r * 0.9, y - r * 0.9, r * 1.8, r * 1.8)
    ctx.fill()
    ctx.stroke()
    // Health: a dark bite out of the dot, from the top, for missing hp.
    if (s.hp < 100) {
      ctx.fillStyle = 'rgba(11,13,16,0.75)'
      ctx.beginPath()
      ctx.moveTo(x, y)
      ctx.arc(x, y, r * 0.62, -Math.PI / 2, -Math.PI / 2 + (Math.PI * 2 * (100 - s.hp)) / 100)
      ctx.closePath()
      ctx.fill()
    }
  }

  // The radar image, or the placeholder, drawn once per size.
  private background(size: number, ratio: number): HTMLCanvasElement {
    const key = `${size}:${ratio}`
    if (this.bg && this.bgKey === key) return this.bg
    const c = this.bg ?? document.createElement('canvas')
    c.width = c.height = Math.max(1, Math.round(size * ratio))
    const g = c.getContext('2d')!
    g.setTransform(ratio, 0, 0, ratio, 0, 0)
    g.fillStyle = '#13161b'
    g.fillRect(0, 0, size, size)
    if (this.radar) {
      g.drawImage(this.radar, 0, 0, size, size)
      if (this.scene.map.twoFloor) note(g, size, 'Both floors are drawn on the main radar')
    } else {
      this.placeholder(g, size)
    }
    this.bg = c
    this.bgKey = key
    return c
  }

  // Dark background, a light grid every 500 game units, the radar's bounds,
  // and a faint trace of everywhere players walked this round.
  private placeholder(g: CanvasRenderingContext2D, size: number) {
    const { map, round } = this.scene
    const k = size / map.size
    const toCss = (x: number, y: number) => worldToRadar(map, x, y).map((v) => v * k) as [number, number]

    // Points are skipped until the player has moved, so standing still
    // doesn't pile up into a bright blob.
    const dot = clamp((70 * k) / map.scale, 2, 10)
    g.fillStyle = 'rgba(150,170,195,0.06)'
    for (const p of round.players) {
      let lx = -1e9
      let ly = -1e9
      for (let i = 0; i < p.x.length; i++) {
        const [x, y] = toCss(p.x[i], p.y[i])
        if (Math.abs(x - lx) < dot / 2 && Math.abs(y - ly) < dot / 2) continue
        g.fillRect(x - dot / 2, y - dot / 2, dot, dot)
        lx = x
        ly = y
      }
    }

    const [left, top] = radarToWorld(map, 0, 0)
    const [right, bottom] = radarToWorld(map, map.size, map.size)
    const step = 500
    g.font = '10px system-ui, sans-serif'
    g.fillStyle = 'rgba(255,255,255,0.3)'
    g.lineWidth = 1
    for (let x = Math.ceil(left / step) * step; x <= right; x += step) {
      const [cx] = toCss(x, 0)
      const major = x % 1000 === 0
      line(g, cx, 0, cx, size, major ? 'rgba(255,255,255,0.12)' : 'rgba(255,255,255,0.05)')
      if (major) {
        g.textAlign = 'left'
        g.textBaseline = 'top'
        g.fillText(String(x), cx + 3, 3)
      }
    }
    for (let y = Math.ceil(bottom / step) * step; y <= top; y += step) {
      const [, cy] = toCss(0, y)
      const major = y % 1000 === 0
      line(g, 0, cy, size, cy, major ? 'rgba(255,255,255,0.12)' : 'rgba(255,255,255,0.05)')
      if (major) {
        g.textAlign = 'left'
        g.textBaseline = 'bottom'
        g.fillText(String(y), 3, cy - 2)
      }
    }
    g.strokeStyle = 'rgba(255,255,255,0.3)'
    g.strokeRect(0.5, 0.5, size - 1, size - 1)
    note(g, size, map.fitted ? 'No radar for this map: positions fitted to the data' : `${map.title}: no radar image`)
  }
}

// A quiet caption in the bottom right corner.
function note(g: CanvasRenderingContext2D, size: number, text: string) {
  g.textBaseline = 'bottom'
  g.textAlign = 'right'
  g.fillStyle = 'rgba(255,255,255,0.4)'
  g.font = '11px system-ui, sans-serif'
  g.fillText(text, size - 6, size - 6)
}

type Pt = (x: number, y: number) => [number, number]

function line(g: CanvasRenderingContext2D, x0: number, y0: number, x1: number, y1: number, color: string) {
  // Half-pixel offsets keep 1px lines sharp.
  const snap = (v: number) => Math.round(v) + 0.5
  g.strokeStyle = color
  g.beginPath()
  g.moveTo(x0 === x1 ? snap(x0) : x0, y0 === y1 ? snap(y0) : y0)
  g.lineTo(x0 === x1 ? snap(x1) : x1, y0 === y1 ? snap(y1) : y1)
  g.stroke()
}

function drawLabel(ctx: CanvasRenderingContext2D, name: string, { x, y, align }: LabelSpot) {
  ctx.textAlign = align
  ctx.lineWidth = 3
  ctx.strokeStyle = 'rgba(0,0,0,0.8)'
  ctx.strokeText(name, x, y)
  ctx.fillStyle = '#eef1f5'
  ctx.fillText(name, x, y)
}

function shorten(name: string): string {
  return name.length > 16 ? `${name.slice(0, 15)}…` : name
}

function detonationPoint(g: Grenade): [number, number] | null {
  return g.path.length ? g.path[g.path.length - 1] : null
}

// Smoke: a circle for its duration. Molotov and incendiary: a fire area.
function drawArea(ctx: CanvasRenderingContext2D, g: Grenade, f: number, hz: number, pt: Pt, unit: number) {
  const smoke = g.type === 'smoke'
  if (!smoke && g.type !== 'molotov' && g.type !== 'incendiary') return
  if (f < g.detonate_frame || f > g.end_frame) return
  const at = detonationPoint(g)
  if (!at) return
  const age = (f - g.detonate_frame) / hz
  const left = (g.end_frame - f) / hz
  const alpha = clamp(age / 0.4, 0, 1) * clamp(left / 1, 0, 1)
  const grow = 0.55 + 0.45 * clamp(age / (smoke ? 0.6 : 0.9), 0, 1)
  const radius = (smoke ? SMOKE_RADIUS : FIRE_RADIUS) * unit * grow
  const [x, y] = pt(at[0], at[1])
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, Math.PI * 2)
  ctx.fillStyle = smoke ? `rgba(200,206,215,${0.6 * alpha})` : `rgba(255,110,30,${0.38 * alpha})`
  ctx.fill()
  ctx.lineWidth = 1.5
  ctx.strokeStyle = smoke ? `rgba(235,238,242,${0.85 * alpha})` : `rgba(255,150,60,${0.9 * alpha})`
  ctx.stroke()
}

// Flash and HE: a short expanding pulse at detonation.
function drawPulse(ctx: CanvasRenderingContext2D, g: Grenade, f: number, hz: number, pt: Pt, unit: number) {
  if (g.type !== 'flash' && g.type !== 'he') return
  const p = (f - g.detonate_frame) / (PULSE_SECONDS * hz)
  if (p < 0 || p >= 1) return
  const at = detonationPoint(g)
  if (!at) return
  const [x, y] = pt(at[0], at[1])
  const radius = (g.type === 'he' ? HE_RADIUS : FLASH_RADIUS) * unit * (0.15 + 0.85 * p)
  const rgb = g.type === 'he' ? '255,106,92' : '255,246,184'
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, Math.PI * 2)
  ctx.fillStyle = `rgba(${rgb},${0.35 * (1 - p)})`
  ctx.fill()
  ctx.lineWidth = 2
  ctx.strokeStyle = `rgba(${rgb},${1 - p})`
  ctx.stroke()
}

// The flight path, drawn up to the grenade's current spot while it flies,
// then faded out. A decoy gets only this.
function drawTrail(ctx: CanvasRenderingContext2D, g: Grenade, f: number, hz: number, pt: Pt) {
  const n = g.path.length
  if (n === 0 || f < g.throw_frame) return
  const flight = Math.max(g.detonate_frame - g.throw_frame, 1)
  const fade = (f - g.detonate_frame) / (TRAIL_FADE_SECONDS * hz)
  if (fade >= 1) return
  const color = GRENADE_COLOR[g.type] ?? '#dddddd'
  // Path index of the grenade now (the path may not be one point per frame).
  const pos = clamp(((f - g.throw_frame) / flight) * (n - 1), 0, n - 1)
  const i = Math.floor(pos)
  const t = pos - i
  const [hx, hy] =
    i + 1 < n
      ? [g.path[i][0] + (g.path[i + 1][0] - g.path[i][0]) * t, g.path[i][1] + (g.path[i + 1][1] - g.path[i][1]) * t]
      : g.path[n - 1]

  ctx.globalAlpha = fade > 0 ? 0.7 * (1 - fade) : 0.9
  ctx.strokeStyle = color
  ctx.lineWidth = 1.5
  ctx.setLineDash([4, 3])
  ctx.beginPath()
  const [x0, y0] = pt(g.path[0][0], g.path[0][1])
  ctx.moveTo(x0, y0)
  for (let j = 1; j <= i; j++) {
    const [x, y] = pt(g.path[j][0], g.path[j][1])
    ctx.lineTo(x, y)
  }
  const [hxc, hyc] = pt(hx, hy)
  ctx.lineTo(hxc, hyc)
  ctx.stroke()
  ctx.setLineDash([])
  if (fade <= 0) {
    ctx.fillStyle = color
    ctx.strokeStyle = '#0b0d10'
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.arc(hxc, hyc, 3, 0, Math.PI * 2)
    ctx.fill()
    ctx.stroke()
  }
  ctx.globalAlpha = 1
}
