import { h } from '../dom'
import { pageTitle, widgetCard } from '../ui'
import { widgets } from '../widgets'

export function renderLab(root: HTMLElement): void {
  root.replaceChildren(
    h('div', { class: 'crumb' }, h('span', { class: 'mono' }, 'Overview')),
    h('h1', { class: 'h1' }, 'Interactive models'),
    h('p', { class: 'lede' }, 'Small working models of the parts that are hardest to follow from prose. Most also appear inside the tour and the page they belong to.'),
    ...widgets.map((w) => h('div', { class: 'lab-item', id: w.name },
      widgetCard(w),
      h('p', { class: 'related' }, 'Related: ', h('a', { href: `#/p/${w.page}` }, pageTitle(w.page))))),
  )
}
