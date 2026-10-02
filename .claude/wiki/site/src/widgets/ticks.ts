// Ticks, frames and seconds, with the schema's conventions (round files are
// now every tick, 64 Hz; try 16 or 32 to see the older plans):
//   frame i is tick start_tick + i * tick_rate / hz
//   a kill's frame is ceil((tick - start_tick) * hz / tick_rate)

import { h } from '../dom'

export function mount(root: HTMLElement): void {
  const field = (label: string, value: number, step = 1) =>
    h('label', {}, label, h('input', { type: 'number', value: String(value), step: String(step) }))
  const rate = field('tick_rate', 64)
  const hz = field('hz', 64)
  const start = field('start_tick', 18000, 64)
  const tick = field('kill tick', 18500)
  const frame = field('frame', 0)
  const out = h('pre', { class: 'mono readout' })
  const num = (l: HTMLElement) => Number(l.querySelector('input')!.value) || 0
  const set = (l: HTMLElement, v: number) => { l.querySelector('input')!.value = String(v) }

  function fromTick() {
    const f = Math.ceil(((num(tick) - num(start)) * num(hz)) / num(rate))
    set(frame, f)
    draw()
  }
  function fromFrame() {
    set(tick, num(start) + (num(frame) * num(rate)) / num(hz))
    draw()
  }
  function draw() {
    const per = num(rate) / num(hz)
    const f = num(frame)
    out.textContent =
      `ticks per frame   ${per}   (${num(rate)} / ${num(hz)})\n` +
      `frame             ${f}   = ceil((${num(tick)} - ${num(start)}) * ${num(hz)} / ${num(rate)})\n` +
      `playback time     ${(f / num(hz)).toFixed(3)} s   (frame / hz; 0 is the end of freeze time)\n` +
      `tick of frame     ${num(start) + f * per}   (start_tick + frame * ticks per frame)\n` +
      (Number.isInteger(per) ? '' : 'Ticks per frame is not a whole number here, so frames fall between ticks.')
  }

  for (const l of [rate, hz, start]) l.addEventListener('input', fromTick)
  tick.addEventListener('input', fromTick)
  frame.addEventListener('input', fromFrame)
  root.append(h('div', { class: 'widget-row' }, rate, hz, start), h('div', { class: 'widget-row' }, tick, frame), out)
  fromTick()
}
