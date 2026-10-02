import { h, shuffle, pct } from '../dom'
import { pageBySlug } from '../content'
import { quizzes, type Question } from '../quizzes'
import { store } from '../store'

interface Item extends Question {
  slug: string
}

const poolFor = (scope: string): Item[] => {
  const slugs = scope === 'all' ? Object.keys(quizzes) : quizzes[scope] ? [scope] : []
  return slugs.flatMap((slug) => quizzes[slug].map((q) => ({ ...q, slug })))
}

export function renderQuiz(root: HTMLElement, scope: string): void {
  const pool = poolFor(scope)
  if (pool.length === 0 || (scope !== 'all' && !quizzes[scope])) return renderPicker(root)
  const items = shuffle(pool)
  let i = 0
  let score = 0
  const missed = new Set<string>()

  const body = h('div', { class: 'quiz' })
  const title = scope === 'all' ? 'Everything' : (pageBySlug(scope)?.title ?? scope)
  root.replaceChildren(
    h('div', { class: 'crumb' }, h('a', { href: '#/study' }, '← Study'), h('span', { class: 'mono' }, 'Quiz')),
    h('h1', { class: 'h1 step-title' }, title),
    body,
  )

  function ask() {
    const item = items[i]
    const options = shuffle(item.options.map((text, k) => ({ text, ok: k === 0 })))
    const why = h('div', { class: 'why', hidden: true })
    const nextBtn = h('button', { hidden: true, onclick: () => { i++; i < items.length ? ask() : finish() } }, i + 1 < items.length ? 'Next question' : 'Show result')
    const buttons = options.map((o) =>
      h('button', {
        class: 'option',
        onclick: (ev: Event) => {
          for (const b of buttons) { b.disabled = true; if (options[buttons.indexOf(b)].ok) b.classList.add('right') }
          const chosen = ev.currentTarget as HTMLButtonElement
          if (o.ok) score++
          else { chosen.classList.add('wrong'); missed.add(item.slug) }
          why.hidden = false
          nextBtn.hidden = false
          nextBtn.focus()
        },
      }, o.text))
    why.append(item.why, ' ', h('a', { href: `#/p/${item.slug}` }, `Read: ${pageBySlug(item.slug)?.title ?? item.slug}`))
    body.replaceChildren(
      h('p', { class: 'muted' }, `Question ${i + 1} of ${items.length}`),
      h('h2', {}, item.q),
      h('div', { class: 'options' }, ...buttons),
      why,
      nextBtn,
    )
  }

  function finish() {
    store.recordQuiz(scope, score, items.length)
    const best = store.get().quiz[scope]
    body.replaceChildren(
      h('h2', {}, `${score} of ${items.length}  (${pct(score, items.length)})`),
      h('p', { class: 'muted' }, `Best score: ${best.best} of ${best.total}.`),
      missed.size
        ? h('p', {}, 'Review: ', ...[...missed].flatMap((s) => [h('a', { href: `#/p/${s}` }, pageBySlug(s)?.title ?? s), ' ']))
        : h('p', {}, 'No questions missed.'),
      h('div', { class: 'widget-row' },
        h('button', { onclick: () => renderQuiz(root, scope) }, 'Retry'),
        h('a', { class: 'btn btn-outline', href: '#/quiz' }, 'All quizzes')),
    )
  }
  ask()
}

export function renderPicker(root: HTMLElement): void {
  const results = store.get().quiz
  const row = (scope: string, name: string, count: number) => {
    const r = results[scope]
    return h('a', { class: 'tile', href: `#/quiz/${scope}` },
      h('b', {}, name),
      h('span', { class: 'muted' }, `${count} questions`),
      h('span', { class: r ? 'chip on' : 'chip' }, r ? `best ${r.best} of ${r.total}` : 'not attempted'))
  }
  const total = Object.values(quizzes).reduce((n, qs) => n + qs.length, 0)
  root.replaceChildren(
    h('div', { class: 'crumb' }, h('a', { href: '#/study' }, '← Study'), h('span', { class: 'mono' }, 'Quizzes')),
    h('h1', { class: 'h1 step-title' }, 'Quizzes'),
    h('p', { class: 'lede' }, 'Questions are drawn from the pages. Each answer links back to the relevant page.'),
    h('div', { class: 'tiles' },
      row('all', 'Everything', total),
      ...Object.keys(quizzes).filter((s) => pageBySlug(s)).map((s) => row(s, pageBySlug(s)!.title, quizzes[s].length))),
  )
}
