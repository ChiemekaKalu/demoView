import { h } from './dom'
import { pageBySlug } from './content'
import type { Widget } from './widgets'
import { mountWidget } from './widgets'

type Kid = Node | string | null | undefined | false

// A framed panel with a small title bar, used for diagrams, models and contents.
export function windowCard(label: string, kids: Kid[], right?: string): HTMLElement {
  return h('div', { class: 'window' },
    h('div', { class: 'titlebar' }, h('i', {}), h('i', {}), h('span', { class: 'mono' }, label), right ? h('span', { class: 'mono right' }, right) : null),
    h('div', { class: 'window-body' }, ...kids))
}

export function tag(text: string, kind: 'sage' | 'tan' | 'plain' = 'plain'): HTMLElement {
  return h('span', { class: `tag ${kind}` }, text)
}

const GROUP_TAG: Record<string, 'sage' | 'tan' | 'plain'> = { Start: 'sage', 'How it works': 'sage', 'Running it': 'tan', Operations: 'tan', Reference: 'plain' }
export const groupTag = (group: string): HTMLElement => tag(group, GROUP_TAG[group] ?? 'plain')

// An interactive model inside a framed panel, mounted into place.
export function widgetCard(w: Widget): HTMLElement {
  const host = h('div', { class: 'widget-body' })
  void mountWidget(w, host)
  return h('section', { class: 'widget' },
    windowCard(`lab / ${w.name}`, [
      h('h3', {}, w.title),
      h('p', { class: 'muted' }, w.blurb),
      host,
    ]))
}

export const pageTitle = (slug: string): string => pageBySlug(slug)?.title ?? slug
