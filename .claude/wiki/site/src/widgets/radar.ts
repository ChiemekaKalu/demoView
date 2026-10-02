// Game coordinates to radar pixels, on the real Mirage radar.
//   px = (x - pos_x) / scale      py = (pos_y - y) / scale

import { h } from '../dom'

const POS_X = -3230
const POS_Y = 1713
const SCALE = 5
const SIZE = 1024

// The radar lives in web/public. It is optional: without it the grid still works.
const images = import.meta.glob('../../../../../web/public/radar/de_mirage.png', { query: '?url', import: 'default', eager: true }) as Record<string, string>
const radarUrl = Object.values(images)[0]

const toPixel = (x: number, y: number) => ({ px: (x - POS_X) / SCALE, py: (POS_Y - y) / SCALE })
const toGame = (px: number, py: number) => ({ x: px * SCALE + POS_X, y: POS_Y - py * SCALE })

export function mount(root: HTMLElement): void {
  const canvas = h('canvas', { width: SIZE, height: SIZE, class: 'radar', 'aria-label': 'Mirage radar. Click to drop a point.' })
  const ctx = canvas.getContext('2d')!
  const img = new Image()
  let ready = false
  let point = { x: 0, y: 0 }

  const inX = h('input', { type: 'number', value: '0', step: '10', 'aria-label': 'Game x' })
  const inY = h('input', { type: 'number', value: '0', step: '10', 'aria-label': 'Game y' })
  const out = h('p', { class: 'mono readout' })

  function draw(hover?: { px: number; py: number }) {
    ctx.clearRect(0, 0, SIZE, SIZE)
    if (ready) ctx.drawImage(img, 0, 0, SIZE, SIZE)
    else {
      ctx.fillStyle = '#1b2430'
      ctx.fillRect(0, 0, SIZE, SIZE)
      ctx.strokeStyle = '#2f3d4f'
      for (let i = 0; i <= SIZE; i += 128) {
        ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, SIZE); ctx.moveTo(0, i); ctx.lineTo(SIZE, i); ctx.stroke()
      }
    }
    const { px, py } = toPixel(point.x, point.y)
    ctx.fillStyle = '#f5b83d'
    ctx.strokeStyle = '#000'
    ctx.lineWidth = 3
    ctx.beginPath(); ctx.arc(px, py, 11, 0, Math.PI * 2); ctx.fill(); ctx.stroke()
    if (hover) {
      ctx.strokeStyle = '#5ab0ff'
      ctx.lineWidth = 2
      ctx.beginPath(); ctx.moveTo(hover.px, 0); ctx.lineTo(hover.px, SIZE); ctx.moveTo(0, hover.py); ctx.lineTo(SIZE, hover.py); ctx.stroke()
    }
    const g = hover ? toGame(hover.px, hover.py) : null
    out.textContent =
      `point  x ${point.x.toFixed(0)}, y ${point.y.toFixed(0)}  ->  px ${px.toFixed(1)}, py ${py.toFixed(1)}` +
      (g ? `\ncursor px ${hover!.px.toFixed(0)}, py ${hover!.py.toFixed(0)}  ->  x ${g.x.toFixed(0)}, y ${g.y.toFixed(0)}` : '')
  }

  const toImage = (e: MouseEvent) => {
    const r = canvas.getBoundingClientRect()
    return { px: ((e.clientX - r.left) / r.width) * SIZE, py: ((e.clientY - r.top) / r.height) * SIZE }
  }

  canvas.addEventListener('mousemove', (e) => draw(toImage(e)))
  canvas.addEventListener('mouseleave', () => draw())
  canvas.addEventListener('click', (e) => {
    const { px, py } = toImage(e)
    point = toGame(px, py)
    inX.value = point.x.toFixed(0)
    inY.value = point.y.toFixed(0)
    draw({ px, py })
  })
  const fromInputs = () => {
    point = { x: Number(inX.value) || 0, y: Number(inY.value) || 0 }
    draw()
  }
  inX.addEventListener('input', fromInputs)
  inY.addEventListener('input', fromInputs)

  if (radarUrl) {
    img.onload = () => { ready = true; draw() }
    img.src = radarUrl
  }
  root.append(
    h('div', { class: 'widget-row' }, h('label', {}, 'x ', inX), h('label', {}, 'y ', inY),
      h('span', { class: 'muted' }, 'Or click the map. Game y points up and image y points down, hence the subtraction.')),
    h('div', { class: 'scroll' }, canvas),
    out,
    ...(radarUrl ? [] : [h('p', { class: 'muted' }, 'web/public/radar/de_mirage.png was not found, so the grid stands in for the radar.')]),
  )
  draw()
}
