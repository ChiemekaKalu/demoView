// Steps through a round's events and shows when the winner becomes known,
// using the same rules as parser/matchlogic/outcome.go (inferOutcome).

import { h } from '../dom'

type Side = 'CT' | 'T'
type Kind = 'death' | 'plant' | 'defuse' | 'explode' | 'restart'

interface Ev {
  t: number // seconds after freeze time ends
  kind: Kind
  side?: Side
}

interface Scenario {
  name: string
  blurb: string
  events: Ev[]
}

interface Verdict {
  at: number // index of the event that revealed it
  t: number // when the round actually ended
  winner: Side
  reason: string
  why: string
}

const START = 5
const ROUND_SECONDS = 115 // mp_roundtime_defuse 1.92 min, floored to whole seconds

const death = (t: number, side: Side): Ev => ({ t, kind: 'death', side })

const SCENARIOS: Scenario[] = [
  {
    name: 'Retake',
    blurb: 'Every T dies after the plant, and the round continues.',
    events: [
      death(14, 'T'), death(22, 'CT'), death(31, 'T'), { t: 38, kind: 'plant' },
      death(47, 'CT'), death(52, 'T'), death(58, 'T'), death(63, 'T'), { t: 81, kind: 'defuse' },
    ],
  },
  {
    name: 'Wipe',
    blurb: 'The last living CT dies, which ends the round as a T win.',
    events: [death(18, 'CT'), death(25, 'T'), death(33, 'CT'), death(40, 'CT'), death(46, 'T'), death(52, 'CT'), death(58, 'CT')],
  },
  {
    name: 'Clock runs out',
    blurb: 'No event decides the round. The timer does.',
    events: [death(20, 'T'), death(30, 'CT'), death(45, 'CT'), death(70, 'T'), { t: 122, kind: 'restart' }],
  },
  {
    name: 'Boom',
    blurb: 'The bomb explodes. The timer no longer applies.',
    events: [death(16, 'CT'), death(29, 'CT'), { t: 36, kind: 'plant' }, death(50, 'T'), death(61, 'CT'), { t: 76, kind: 'explode' }],
  },
]

const label = (e: Ev): string => {
  switch (e.kind) {
    case 'death': return `${e.side} player dies`
    case 'plant': return 'bomb_planted'
    case 'defuse': return 'bomb_defused'
    case 'explode': return 'bomb_exploded'
    case 'restart': return 'next round starts'
  }
}

const clock = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`

// Mirrors inferOutcome: the first thing that decides the round wins.
function decide(events: Ev[]): Verdict | null {
  const alive = { CT: START, T: START }
  let planted = false
  for (let i = 0; i < events.length; i++) {
    const e = events[i]
    if (!planted && e.t > ROUND_SECONDS) {
      return { at: i, t: ROUND_SECONDS, winner: 'CT', reason: 'time_expired', why: 'The timer ran out with the bomb not planted.' }
    }
    switch (e.kind) {
      case 'death':
        alive[e.side!]--
        if (alive.CT === 0) return { at: i, t: e.t, winner: 'T', reason: 'elimination', why: 'The last living CT died.' }
        if (alive.T === 0 && !planted) return { at: i, t: e.t, winner: 'CT', reason: 'elimination', why: 'The last living T died, and the bomb was not planted.' }
        break
      case 'plant': planted = true; break
      case 'defuse': return { at: i, t: e.t, winner: 'CT', reason: 'bomb_defused', why: 'The bomb was defused.' }
      case 'explode': return { at: i, t: e.t, winner: 'T', reason: 'bomb_exploded', why: 'The bomb exploded.' }
    }
  }
  return null
}

export function mount(root: HTMLElement): void {
  let sc = SCENARIOS[0]
  let step = 0 // events applied so far
  let timer = 0

  const picker = h('select', { 'aria-label': 'Scenario' }, ...SCENARIOS.map((s, i) => h('option', { value: String(i) }, s.name)))
  const blurb = h('p', { class: 'muted' })
  const board = h('div', { class: 'sim-board' })
  const log = h('ol', { class: 'sim-log' })
  const verdictBox = h('div', { class: 'sim-verdict' })
  const next = h('button', { onclick: () => advance() }, 'Next event')
  const play = h('button', { class: 'ghost', onclick: () => toggleAuto() }, 'Play')
  const reset = h('button', { class: 'ghost', onclick: () => { stop(); step = 0; draw() } }, 'Reset')

  function stop() {
    window.clearInterval(timer)
    timer = 0
    play.textContent = 'Play'
  }
  function advance() {
    if (step < sc.events.length) step++
    if (step >= sc.events.length) stop()
    draw()
  }
  function toggleAuto() {
    if (timer) return stop()
    if (step >= sc.events.length) step = 0
    play.textContent = 'Pause'
    timer = window.setInterval(advance, 900)
    advance()
  }

  function dots(side: Side, living: number): HTMLElement {
    return h('div', { class: `sim-side ${side}` },
      h('span', { class: 'sim-name' }, side),
      ...Array.from({ length: START }, (_, i) => h('i', { class: i < living ? 'alive' : 'dead' })),
    )
  }

  function draw() {
    const applied = sc.events.slice(0, step)
    const alive = { CT: START, T: START }
    let planted = false
    for (const e of applied) {
      if (e.kind === 'death') alive[e.side!]--
      if (e.kind === 'plant') planted = true
    }
    const v = decide(sc.events)
    const known = v !== null && step > v.at
    const now = step === 0 ? 0 : applied[applied.length - 1].t
    const shown = known ? v.t : now

    blurb.textContent = sc.blurb
    board.replaceChildren(
      dots('CT', alive.CT),
      dots('T', alive.T),
      h('div', { class: 'sim-meta' },
        h('span', { class: planted ? 'chip hot' : 'chip' }, planted ? 'Bomb planted' : 'No bomb'),
        h('span', { class: 'chip' }, `Clock ${clock(Math.min(shown, ROUND_SECONDS))} of ${clock(ROUND_SECONDS)}`),
      ),
      h('div', { class: 'sim-bar' },
        h('div', { class: 'fill', style: `width:${Math.min(100, (shown / ROUND_SECONDS) * 100)}%` }),
      ),
    )
    log.replaceChildren(...sc.events.map((e, i) => h('li', { class: i < step ? 'done' : i === step ? 'next' : '' },
      h('span', { class: 'mono' }, clock(e.t)), ' ', label(e),
      v && i === v.at ? h('b', { class: 'tag' }, 'decides it') : null,
    )))
    verdictBox.replaceChildren(
      known
        ? h('div', { class: `callout ${v.winner}` },
            h('b', {}, `${v.winner} win, ${v.reason}`),
            h('span', {}, ` at ${clock(v.t)}. ${v.why}`),
            v.reason === 'time_expired'
              ? h('span', { class: 'muted' }, ' The expiry is only noticed when the next event arrives, and the end tick is set back to the timer.')
              : null)
        : h('div', { class: 'callout' }, h('span', { class: 'muted' }, step === 0 ? 'Step through the round. The result appears when a rule applies.' : 'The round is still undecided.')),
    )
    next.toggleAttribute('disabled', step >= sc.events.length)
  }

  picker.addEventListener('change', () => { stop(); sc = SCENARIOS[Number(picker.value)]; step = 0; draw() })
  root.append(h('div', { class: 'widget-row' }, picker, next, play, reset), blurb, board, log, verdictBox)
  draw()
}
