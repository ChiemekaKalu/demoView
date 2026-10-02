import { h } from '../dom'
import { diagram, NODES } from '../diagram'
import { pages } from '../content'
import { renderInline } from '../markdown'
import { STEPS } from '../tour'
import { widgets, widgetByName } from '../widgets'
import { widgetCard, windowCard } from '../ui'

const readHref = (to: string) => `#/p/${to.replace('#', '/')}`
const two = (n: number) => String(n).padStart(2, '0')

export function renderLanding(root: HTMLElement): void {
  const d = diagram()
  d.set({ nodes: [], edges: [] })

  const explore: [title: string, href: string, text: string, meta: string][] = [
    ['System map', '#/map', 'Each component of the system: what it does, where it lives in the repository, and what to read next.', `${NODES.length} components`],
    ['Lab', '#/lab', 'Interactive models of the parts that are hardest to picture from prose alone.', `${widgets.length} models`],
    ['Pages', '#/pages', 'The full write-up: parser, match logic, schema, Worker, viewer, testing and infrastructure.', `${pages.length} pages`],
    ['Study', '#/study', 'Optional progress tracking, quizzes, flashcards and notes.', 'stored in this browser'],
  ]

  root.replaceChildren(
    h('section', { class: 'hero' },
      h('h1', { class: 'h1' }, 'How a CS2 demo becomes a replay'),
      h('p', { class: 'lede' }, 'A demo file goes in, and a scoreboard and a scrubbable 2D replay of every round come out. This tour follows one upload through each component, with a model to explore at the key steps.'),
      h('div', { class: 'buttons' },
        h('a', { class: 'btn btn-dark', href: '#/tour/1' }, 'Start the tour'),
        h('a', { class: 'btn btn-outline', href: '#/map' }, 'Open the system map'))),
    windowCard('system.svg', [d.el], `${NODES.length} components`),

    h('section', { class: 'section' },
      h('div', { class: 'section-head' }, h('h2', { class: 'h2' }, 'The path'), h('span', { class: 'count mono' }, `${STEPS.length} steps`)),
      h('ol', { class: 'timeline' }, ...STEPS.map((s, i) =>
        h('li', {},
          h('span', { class: 'dot' }),
          h('a', { class: 'stop', href: `#/tour/${i + 1}` },
            h('span', { class: 'role-head' }, h('h3', {}, s.title), h('span', { class: 'years mono' }, two(i + 1))),
            h('p', {}, s.summary)))))),

    h('section', { class: 'section' },
      h('div', { class: 'section-head' }, h('h2', { class: 'h2' }, 'Explore')),
      h('div', { class: 'project-grid' }, ...explore.map(([title, href, text, meta]) =>
        h('a', { class: 'project', href },
          h('span', { class: 'project-head' }, h('h3', {}, title), h('span', { class: 'arrow' }, '→')),
          h('p', {}, text),
          h('span', { class: 'stack mono' }, meta))))),
  )
}

export function renderStep(root: HTMLElement, n: number): void {
  const i = Math.min(Math.max(n, 1), STEPS.length) - 1
  const step = STEPS[i]
  const d = diagram()
  d.set(step.active)

  const w = step.widget ? widgetByName(step.widget) : undefined
  const go = (to: number) => { location.hash = `#/tour/${to}` }
  const prev = i > 0 ? h('button', { class: 'btn-outline', onclick: () => go(i) }, 'Previous') : h('span', {})
  const next = i < STEPS.length - 1
    ? h('button', { onclick: () => go(i + 2) }, 'Next')
    : h('a', { class: 'btn btn-dark', href: '#/map' }, 'Open the system map')

  root.replaceChildren(
    h('div', { class: 'crumb' }, h('a', { href: '#/' }, '← Overview'), h('span', { class: 'mono' }, `Step ${i + 1} of ${STEPS.length}`)),
    h('h1', { class: 'h1 step-title' }, step.title),
    windowCard('system.svg', [d.el], `step ${two(i + 1)}`),
    h('ol', { class: 'steps-bar', 'aria-label': 'Steps' }, ...STEPS.map((s, k) =>
      h('li', {}, h('a', { href: `#/tour/${k + 1}`, class: k === i ? 'on' : k < i ? 'seen' : '', title: s.title, 'aria-label': `Step ${k + 1}: ${s.title}`, 'aria-current': k === i ? 'step' : null })))),
    h('p', { class: 'lede', html: renderInline(step.body) }),
    ...(step.facts ? [h('ul', { class: 'facts' }, ...step.facts.map((f) => h('li', { html: renderInline(f) })))] : []),
    ...(w ? [widgetCard(w)] : []),
    h('div', { class: 'read' }, h('span', { class: 'eyebrow' }, 'Read more'), ...step.read.map(([label, to]) => h('a', { class: 'pill', href: readHref(to) }, label))),
    h('div', { class: 'step-nav' }, prev, h('span', { class: 'muted mono' }, 'Left and right arrow keys also work'), next),
  )

  const onKey = (e: KeyboardEvent) => {
    if (!root.isConnected || !location.hash.startsWith('#/tour/')) return document.removeEventListener('keydown', onKey)
    if ((e.target as HTMLElement).closest('input, textarea, select')) return
    if (e.key === 'ArrowRight' && i < STEPS.length - 1) go(i + 2)
    if (e.key === 'ArrowLeft' && i > 0) go(i)
  }
  document.addEventListener('keydown', onKey)
}
