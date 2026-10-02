import { h } from '../dom'
import { groups, latestChange, pages } from '../content'
import { renderMarkdown } from '../markdown'
import { store } from '../store'
import { groupTag } from '../ui'

// The filter and query survive navigating away and back within a visit.
let filter = 'All'
let query = ''

export function renderPages(root: HTMLElement): void {
  const list = h('div', { class: 'post-list' })
  const chips = h('div', { class: 'filters', role: 'group', 'aria-label': 'Filter by section' })
  const empty = h('p', { class: 'muted', hidden: true }, 'No pages match.')

  const matches = (p: (typeof pages)[number]) =>
    (filter === 'All' || p.group === filter) &&
    (!query || p.title.toLowerCase().includes(query) || p.md.toLowerCase().includes(query))

  function draw() {
    const shown = pages.filter(matches)
    chips.replaceChildren(...['All', ...groups].map((g) => {
      const n = g === 'All' ? pages.length : pages.filter((p) => p.group === g).length
      return h('button', { class: 'filter', 'aria-pressed': String(g === filter), onclick: () => { filter = g; draw() } },
        g, h('span', { class: 'filter-count mono' }, String(n)))
    }))
    list.replaceChildren(...shown.map((p) =>
      h('a', { class: 'post-row', href: `#/p/${p.slug}` },
        h('span', { class: 'post-date mono' }, `${p.minutes} min`),
        h('span', { class: 'post-body' },
          h('h3', {}, p.title),
          h('p', {}, p.description),
          h('span', { class: 'post-meta' }, groupTag(p.group), store.isLearned(p.slug) ? h('span', { class: 'topic mono' }, 'read') : null)))))
    empty.hidden = shown.length > 0
  }

  const search = h('input', { type: 'search', class: 'search', placeholder: 'Search pages', 'aria-label': 'Search pages', value: query, autocomplete: 'off' })
  search.addEventListener('input', () => { query = search.value.trim().toLowerCase(); draw() })

  root.replaceChildren(
    h('div', { class: 'crumb' }, h('span', { class: 'mono' }, 'Documentation')),
    h('div', { class: 'section-head head-first' }, h('h1', { class: 'h1' }, 'Pages'), h('span', { class: 'count mono' }, `${pages.length} pages`)),
    h('p', { class: 'lede' }, 'The full write-up, in an order where each page builds on the one before.'),
    chips,
    search,
    list,
    empty,
    h('section', { class: 'section' },
      h('div', { class: 'section-head' }, h('h2', { class: 'h2' }, 'Latest change')),
      h('div', { class: 'prose', html: renderMarkdown(latestChange(), 'changelog') })),
  )
  draw()
}
