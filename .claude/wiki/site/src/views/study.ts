// The study tab: progress, flashcards, quizzes and notes. Everything here is
// optional, and everything is kept in this browser.

import { h, pct } from '../dom'
import { pages } from '../content'
import { quizzes } from '../quizzes'
import { store } from '../store'
import { dueCards } from './cards'

export function renderStudy(root: HTMLElement): void {
  const read = pages.filter((p) => store.isLearned(p.slug)).length
  const due = dueCards().length
  const taken = Object.keys(store.get().quiz).length

  const row = (slug: string) => {
    const p = pages.find((x) => x.slug === slug)!
    const note = h('textarea', { rows: '4', placeholder: 'Notes on this page', 'aria-label': `Notes on ${p.title}`, hidden: !store.get().notes[slug] })
    note.value = store.get().notes[slug] ?? ''
    note.addEventListener('input', () => store.setNote(slug, note.value))
    const box = h('input', { type: 'checkbox', id: `l-${slug}`, 'aria-label': `${p.title}: mark as read` })
    box.checked = store.isLearned(slug)
    box.addEventListener('change', () => { store.toggleLearned(slug); renderStudy(root) })
    return h('li', { class: 'study-row' },
      h('div', { class: 'study-line' },
        box,
        h('a', { href: `#/p/${slug}` }, p.title),
        h('span', { class: 'mono meta' }, `${p.minutes} min`),
        quizzes[slug] ? h('a', { class: 'mono meta', href: `#/quiz/${slug}` }, `quiz (${quizzes[slug].length})`) : h('span', {}),
        h('button', { class: 'btn-outline small', onclick: () => { note.hidden = !note.hidden; if (!note.hidden) note.focus() } }, store.get().notes[slug] ? 'Notes *' : 'Notes')),
      note)
  }

  root.replaceChildren(
    h('div', { class: 'crumb' }, h('span', { class: 'mono' }, 'Optional')),
    h('h1', { class: 'h1' }, 'Study'),
    h('p', { class: 'lede' }, 'Track what you have read, test recall with quizzes and flashcards, and keep notes. Nothing here is required, and everything is stored in this browser.'),
    h('div', { class: 'stats' },
      h('div', { class: 'stat' },
        h('span', { class: 'stat-label mono' }, 'READ'),
        h('span', { class: 'stat-value' }, `${read} of ${pages.length}`),
        h('div', { class: 'bar' }, h('div', { class: 'fill', style: `width:${pct(read, pages.length)}` }))),
      h('a', { class: 'stat link', href: '#/cards' },
        h('span', { class: 'stat-label mono' }, 'FLASHCARDS'),
        h('span', { class: 'stat-value' }, String(due)),
        h('span', { class: 'muted' }, due === 1 ? 'card due' : 'cards due')),
      h('a', { class: 'stat link', href: '#/quiz' },
        h('span', { class: 'stat-label mono' }, 'QUIZZES'),
        h('span', { class: 'stat-value' }, `${taken} of ${Object.keys(quizzes).length}`),
        h('span', { class: 'muted' }, 'attempted'))),
    h('section', { class: 'section' },
      h('div', { class: 'section-head' }, h('h2', { class: 'h2' }, 'Pages and notes')),
      h('ul', { class: 'study-list' }, ...pages.map((p) => row(p.slug)))),
    h('p', { class: 'muted reset' }, 'Progress, notes and scores are stored in this browser. ',
      h('button', { class: 'btn-outline small', onclick: () => { if (window.confirm?.('Clear progress, notes and scores?') ?? true) { store.reset(); renderStudy(root) } } }, 'Reset')),
  )
}
