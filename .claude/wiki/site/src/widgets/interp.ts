// What the viewer does between two data frames: interpolate yaw along the
// shorter arc (lerpAngle in web/src/lib/interp.ts), and draw at the display's
// own rate, which rarely matches the data's.

import { h } from '../dom'

const wrap = (d: number) => ((d % 360) + 360) % 360

function arc(a: number, b: number, t: number): number {
  let diff = wrap(b - a)
  if (diff > 180) diff -= 360
  return wrap(a + diff * t)
}
const naive = (a: number, b: number, t: number) => a + (b - a) * t

// yaw 0 points along +x and angles run counterclockwise; canvas y points down.
function dial(label: string, a: number, b: number, yaw: number): HTMLCanvasElement {
  const c = h('canvas', { width: 200, height: 200, class: 'dial', 'aria-label': label })
  const g = c.getContext('2d')!
  const css = getComputedStyle(document.documentElement)
  const col = (n: string) => css.getPropertyValue(n).trim()
  const ray = (deg: number, color: string, w: number, len = 78) => {
    const r = (deg * Math.PI) / 180
    g.strokeStyle = color
    g.lineWidth = w
    g.beginPath()
    g.moveTo(100, 100)
    g.lineTo(100 + Math.cos(r) * len, 100 - Math.sin(r) * len)
    g.stroke()
  }
  g.strokeStyle = col('--line')
  g.lineWidth = 1
  g.beginPath(); g.arc(100, 100, 78, 0, Math.PI * 2); g.stroke()
  ray(a, col('--ct'), 2)
  ray(b, col('--t'), 2)
  ray(yaw, col('--fg'), 5, 60)
  g.fillStyle = col('--muted')
  g.font = '12px system-ui, sans-serif'
  g.fillText(label, 8, 16)
  return c
}

export function mount(root: HTMLElement): void {
  const slider = (label: string, min: number, max: number, v: number) =>
    h('label', {}, label, h('input', { type: 'range', min: String(min), max: String(max), value: String(v) }))
  const A = slider('frame A yaw', 0, 359, 350)
  const B = slider('frame B yaw', 0, 359, 20)
  const T = slider('t between frames, percent', 0, 100, 50)
  const dials = h('div', { class: 'dials' })
  const readout = h('p', { class: 'mono readout' })
  const rates = h('div', { class: 'strip' })
  const refresh = h('select', { 'aria-label': 'Display refresh rate' }, ...[30, 60, 120, 144].map((r) => h('option', { value: String(r), selected: r === 60 }, `${r} Hz`)))
  const data = h('select', { 'aria-label': 'Round file rate' }, ...[16, 32, 64].map((r) => h('option', { value: String(r), selected: r === 64 }, `${r} Hz`)))
  const v = (l: HTMLElement) => Number(l.querySelector('input')!.value)

  function draw() {
    const a = v(A), b = v(B), t = v(T) / 100
    const n = naive(a, b, t), s = arc(a, b, t)
    dials.replaceChildren(dial('naive lerp', a, b, n), dial('shorter arc', a, b, s))
    readout.textContent =
      `blue = frame A, amber = frame B, white = drawn\n` +
      `naive lerp   ${wrap(n).toFixed(0)} deg   (swings ${Math.abs(b - a).toFixed(0)} deg to get there)\n` +
      `shorter arc  ${s.toFixed(0)} deg   (turns ${Math.abs(((wrap(b - a) + 180) % 360) - 180).toFixed(0)} deg)`

    const hz = Number(refresh.value)
    const marks: HTMLElement[] = []
    const rate = Number(data.value)
    for (let i = 0; i < rate; i++) marks.push(h('i', { class: 'data', style: `left:${(i / rate) * 100}%` }))
    for (let i = 0; i < hz; i++) marks.push(h('i', { class: 'draw', style: `left:${(i / hz) * 100}%` }))
    rates.replaceChildren(...marks)
    rates.dataset.caption = `One second: ${rate} data frames (tall), ${hz} screen draws (short). ${hz > rate ? `About ${(hz / rate).toFixed(2)} draws per data frame, each at its own interpolated position.` : `The screen draws less often than the data arrives, so frames are skipped and interpolation barely matters.`}`
  }
  for (const l of [A, B, T]) l.addEventListener('input', draw)
  refresh.addEventListener('change', draw)
  data.addEventListener('change', draw)
  root.append(
    h('div', { class: 'widget-row' }, A, B, T),
    dials,
    readout,
    h('div', { class: 'widget-row' }, h('label', {}, 'round file ', data), h('label', {}, 'display ', refresh)),
    rates,
  )
  draw()
}
