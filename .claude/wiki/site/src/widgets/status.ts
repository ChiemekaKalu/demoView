// The match status machine from worker/src/consumer.ts: push a match
// through it and watch what the queue and the dead letter queue do.

import { h } from '../dom'

type Status = 'uploaded' | 'queued' | 'parsing' | 'ready' | 'failed'

const STATES: Status[] = ['uploaded', 'queued', 'parsing', 'ready', 'failed']
const MAX_ATTEMPTS = 3

export function mount(root: HTMLElement): void {
  let status: Status = 'uploaded'
  let attempts = 0
  let lines: string[] = []

  const chips = h('div', { class: 'status-row' })
  const info = h('p', { class: 'muted' })
  const buttons = h('div', { class: 'widget-row' })
  const log = h('pre', { class: 'log mono' })

  const say = (s: string) => { lines = [s, ...lines].slice(0, 8) }
  const to = (next: Status, why: string) => {
    say(`${status} -> ${next}  ${why}`)
    status = next
  }

  const actions: { label: string; when: (s: Status) => boolean; run: () => void }[] = [
    { label: 'Confirm upload', when: (s) => s === 'uploaded', run: () => to('queued', 'POST /uploaded: object exists, job enqueued') },
    { label: 'Consumer starts', when: (s) => s === 'queued', run: () => { attempts++; to('parsing', `attempt ${attempts} of ${MAX_ATTEMPTS}`) } },
    { label: 'Parser says 200', when: (s) => s === 'parsing', run: () => to('ready', 'valid match.json, rows saved to D1, message acked') },
    { label: '200 but bad output', when: (s) => s === 'parsing', run: () => to('failed', 'bad parser output: retrying would return the same thing') },
    { label: 'Parser says 422', when: (s) => s === 'parsing', run: () => to('failed', 'the demo is bad, no retry') },
    {
      label: '5xx or timeout',
      when: (s) => s === 'parsing',
      run: () => {
        if (attempts < MAX_ATTEMPTS) to('queued', 'transient: retry in 30 s')
        else to('failed', `dead letter queue: parse failed after ${MAX_ATTEMPTS} attempts`)
      },
    },
    { label: 'Redeliver message', when: (s) => s === 'ready' || s === 'failed', run: () => say(`job_skipped  status is ${status}, so the message is acked and ignored`) },
  ]

  function draw() {
    chips.replaceChildren(...STATES.map((s) => h('span', { class: `chip st-${s}${s === status ? ' on' : ''}` }, s)))
    info.textContent = `Attempts used: ${attempts} of ${MAX_ATTEMPTS}. After the last one, a transient error goes to the dead letter queue.`
    buttons.replaceChildren(
      ...actions.map((a) => h('button', { disabled: !a.when(status), onclick: () => { a.run(); draw() } }, a.label)),
      h('button', { class: 'ghost', onclick: () => { status = 'uploaded'; attempts = 0; lines = []; draw() } }, 'New match'),
    )
    log.textContent = lines.length ? lines.join('\n') : 'No events yet.'
  }
  root.append(chips, info, buttons, log)
  draw()
}
