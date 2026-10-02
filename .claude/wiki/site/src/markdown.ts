import { Marked } from 'marked'

export function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/<[^>]+>/g, '')
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .trim()
    .replace(/\s/g, '-')
}

const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

let seen = new Map<string, number>()
let currentSlug = ''

const marked = new Marked({
  gfm: true,
  renderer: {
    // Heading ids follow GitHub's, so `page.md#some-heading` links work here too.
    heading({ tokens, depth }) {
      const inner = this.parser.parseInline(tokens)
      const base = slugify(inner)
      const n = seen.get(base) ?? 0
      seen.set(base, n + 1)
      const id = n === 0 ? base : `${base}-${n}`
      return `<h${depth} id="${id}">${inner}</h${depth}>\n`
    },
    code({ text, lang }) {
      if (lang === 'mermaid') return `<pre class="mermaid">${escapeHtml(text)}</pre>\n`
      return false
    },
    // `other.md#heading` becomes a route in this app. External links open a new tab.
    link({ href, title, tokens }) {
      const text = this.parser.parseInline(tokens)
      const t = title ? ` title="${escapeHtml(title)}"` : ''
      const local = /^([\w-]+)\.md(#[\w-]+)?$/.exec(href)
      if (local) {
        const frag = local[2] ? `/${local[2].slice(1)}` : ''
        return `<a href="#/p/${local[1].toLowerCase()}${frag}"${t}>${text}</a>`
      }
      if (/^https?:/.test(href)) return `<a href="${href}"${t} target="_blank" rel="noreferrer">${text}</a>`
      if (href.startsWith('#')) return `<a href="#/p/${currentSlug}/${href.slice(1)}"${t}>${text}</a>`
      return `<a href="${href}"${t}>${text}</a>`
    },
  },
})

export function renderMarkdown(md: string, slug = ''): string {
  seen = new Map()
  currentSlug = slug
  return marked.parse(md, { async: false })
}

export function renderInline(md: string): string {
  return marked.parseInline(md, { async: false })
}

let mermaidReady: Promise<typeof import('mermaid').default> | null = null

// Draws every `pre.mermaid` under root. The library is large, so it loads
// only when a page has a diagram.
export async function drawDiagrams(root: HTMLElement): Promise<void> {
  const nodes = [...root.querySelectorAll<HTMLElement>('pre.mermaid')]
  if (nodes.length === 0) return
  mermaidReady ??= import('mermaid').then((m) => m.default)
  const mermaid = await mermaidReady
  // Colours come from the page's own tokens, so diagrams follow the theme.
  const css = getComputedStyle(document.documentElement)
  const v = (name: string) => css.getPropertyValue(name).trim()
  mermaid.initialize({
    startOnLoad: false,
    theme: 'base',
    securityLevel: 'strict',
    fontFamily: v('--sans'),
    flowchart: { useMaxWidth: true, htmlLabels: true, curve: 'basis' },
    sequence: { useMaxWidth: true },
    themeVariables: {
      fontSize: '14px',
      background: v('--surface'),
      primaryColor: v('--surface-alt'),
      primaryBorderColor: v('--sage-border'),
      primaryTextColor: v('--text'),
      secondaryColor: v('--hover-fill'),
      tertiaryColor: v('--surface'),
      lineColor: v('--meta'),
      textColor: v('--text'),
      clusterBkg: v('--surface'),
      clusterBorder: v('--border-ctl'),
      edgeLabelBackground: v('--surface'),
      noteBkgColor: v('--surface-alt'),
      noteTextColor: v('--text'),
      noteBorderColor: v('--border-ctl'),
      actorBkg: v('--surface-alt'),
      actorBorder: v('--sage-border'),
      actorTextColor: v('--text'),
      signalColor: v('--meta'),
      signalTextColor: v('--text'),
      labelBoxBkgColor: v('--surface-alt'),
      labelBoxBorderColor: v('--border-ctl'),
      labelTextColor: v('--text'),
      loopTextColor: v('--text'),
      activationBkgColor: v('--hover-fill'),
      activationBorderColor: v('--sage-border'),
    },
  })
  try {
    await mermaid.run({ nodes })
  } catch (err) {
    console.error('mermaid', err)
  }
}
