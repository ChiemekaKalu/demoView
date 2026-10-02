// Flashcards from the glossary, on a simple spaced schedule: a card you know
// moves to a longer wait, a card you miss comes back soon.

import { h, shuffle } from '../dom'
import { glossaryTerms, type Term } from '../content'
import { renderInline } from '../markdown'
import { store } from '../store'

const MIN = 60_000
const DAY = 24 * 60 * MIN
const WAITS = [10 * MIN, DAY, 3 * DAY, 7 * DAY]

export const dueCards = (): Term[] => {
  const now = Date.now()
  return glossaryTerms().filter((t) => (store.get().cards[t.id]?.due ?? 0) <= now)
}

export function renderCards(root: HTMLElement, all = false): void {
  const terms = glossaryTerms()
  const queue = shuffle(all ? terms : dueCards())
  const boxes = [0, 0, 0, 0]
  for (const t of terms) boxes[Math.min(store.get().cards[t.id]?.box ?? 0, 3)]++

  const stage = h('div', { class: 'card-stage' })
  root.replaceChildren(
    h('div', { class: 'crumb' }, h('a', { href: '#/study' }, '← Study'), h('span', { class: 'mono' }, 'Flashcards')),
    h('h1', { class: 'h1 step-title' }, 'Flashcards'),
    h('p', { class: 'lede' }, `${terms.length} terms from the glossary. New: ${boxes[0]}, learning: ${boxes[1]}, familiar: ${boxes[2]}, known: ${boxes[3]}.`),
    stage,
  )

  let flipped = false
  const onKey = (e: KeyboardEvent) => {
    if (!stage.isConnected) return document.removeEventListener('keydown', onKey)
    if ((e.target as HTMLElement).closest('input, textarea, select')) return
    if (e.key === ' ') { e.preventDefault(); if (!flipped) stage.querySelector<HTMLButtonElement>('.flip')?.click() }
    else if (flipped && e.key === '1') stage.querySelector<HTMLButtonElement>('.again')?.click()
    else if (flipped && e.key === '2') stage.querySelector<HTMLButtonElement>('.know')?.click()
  }
  document.addEventListener('keydown', onKey)

  function show() {
    flipped = false
    const t = queue[0]
    if (!t) {
      stage.replaceChildren(
        h('div', { class: 'callout' }, h('b', {}, all ? 'All cards reviewed.' : 'No cards are due.')),
        h('div', { class: 'widget-row' }, h('button', { onclick: () => renderCards(root, true) }, 'Review all cards'), h('a', { class: 'btn btn-outline', href: '#/study' }, 'Back to Study')),
      )
      return
    }
    const answer = h('div', { class: 'card-answer', hidden: true, html: renderInline(t.definition) })
    const rate = h('div', { class: 'widget-row', hidden: true },
      h('button', { class: 'again btn-outline', onclick: () => grade(t, false) }, 'Again (1)'),
      h('button', { class: 'know', onclick: () => grade(t, true) }, 'Got it (2)'))
    const flip = h('button', { class: 'flip', onclick: () => { flipped = true; answer.hidden = false; rate.hidden = false; flip.hidden = true } }, 'Show answer (space)')
    stage.replaceChildren(
      h('div', { class: 'card' },
        h('div', { class: 'card-term', html: renderInline(t.term) }),
        answer),
      flip, rate,
      h('p', { class: 'muted' }, `${queue.length} remaining`),
    )
  }

  function grade(t: Term, knew: boolean) {
    const box = store.get().cards[t.id]?.box ?? 0
    const next = knew ? Math.min(box + 1, 3) : 0
    store.setCard(t.id, { box: next, due: Date.now() + WAITS[next] })
    queue.shift()
    if (!knew) queue.push(t)
    show()
  }
  show()
}
