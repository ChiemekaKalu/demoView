import { h } from '../dom'
import { pageBySlug, pages } from '../content'
import { drawDiagrams, renderMarkdown } from '../markdown'
import { quizzes } from '../quizzes'
import { groupTag, widgetCard, windowCard } from '../ui'
import { widgetByName } from '../widgets'

export function renderPage(root: HTMLElement, slug: string, frag: string): void {
  const page = pageBySlug(slug)
  if (!page) {
    root.replaceChildren(h('h1', { class: 'h1' }, 'Page not found'), h('p', {}, h('a', { href: '#/pages' }, 'Back to all pages')))
    return
  }

  // The title is shown in the header, so the body starts after the first heading.
  const body = h('article', { class: 'prose', html: renderMarkdown(page.md.replace(/^#\s+.+\r?\n/, ''), slug) })

  // `<!-- widget: name -->` becomes an interactive model.
  const found: Comment[] = []
  const walker = document.createTreeWalker(body, NodeFilter.SHOW_COMMENT)
  for (let n = walker.nextNode(); n; n = walker.nextNode()) found.push(n as Comment)
  for (const c of found) {
    const w = widgetByName(/^\s*widget:\s*([\w-]+)\s*$/.exec(c.data)?.[1] ?? '')
    if (w) c.replaceWith(widgetCard(w))
  }
  for (const t of body.querySelectorAll('table')) t.replaceWith(h('div', { class: 'scroll table-wrap' }, t.cloneNode(true)))
  for (const m of body.querySelectorAll('pre.mermaid')) m.replaceWith(windowCard('diagram', [m.cloneNode(true)]))
  void drawDiagrams(body)

  const heads = [...body.querySelectorAll<HTMLElement>('h2, h3')]
  const contents = heads.filter((e) => e.tagName === 'H2').length >= 3
    ? windowCard('contents', [h('ul', { class: 'toc' }, ...heads.map((e) =>
        h('li', { class: e.tagName === 'H3' ? 'sub' : '' }, h('a', { href: `#/p/${slug}/${e.id}` }, e.textContent ?? ''))))])
    : null

  const i = pages.indexOf(page)
  const prev = pages[i - 1]
  const next = pages[i + 1]
  const quiz = quizzes[slug]

  root.replaceChildren(
    h('div', { class: 'crumb' }, h('a', { href: '#/pages' }, '← All pages'), h('span', { class: 'mono' }, `${page.minutes} min read`)),
    h('div', { class: 'meta-row' }, groupTag(page.group)),
    h('h1', { class: 'h1 article-title' }, page.title),
    ...(contents ? [contents] : []),
    body,
    h('div', { class: 'cta-card' },
      h('span', {}, quiz ? `${quiz.length} questions on this page.` : 'Continue with the next page, or return to the list.'),
      quiz ? h('a', { class: 'btn btn-dark', href: `#/quiz/${slug}` }, 'Take the quiz') : h('a', { class: 'btn btn-outline', href: '#/pages' }, 'All pages')),
    h('nav', { class: 'pager' },
      prev ? h('a', { href: `#/p/${prev.slug}` }, h('span', { class: 'mono' }, 'Previous'), h('b', {}, prev.title)) : h('span', {}),
      next ? h('a', { class: 'next', href: `#/p/${next.slug}` }, h('span', { class: 'mono' }, 'Next'), h('b', {}, next.title)) : h('span', {})),
  )

  if (frag) {
    // Diagrams change the height above the target, so scroll again once they settle.
    const go = () => document.getElementById(frag)?.scrollIntoView()
    go()
    setTimeout(go, 400)
  } else scrollTo(0, 0)
}
