// The wiki pages are the markdown files in the directory above. Any new
// file there shows up on its own, under "More".

const files = import.meta.glob('../../*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>

export interface Page {
  slug: string
  title: string
  description: string
  md: string
  minutes: number
  group: string
}

// The learning path, in reading order, and the group each page sits in.
const PATH: [slug: string, group: string][] = [
  ['readme', 'Start'],
  ['big-picture', 'Start'],
  ['demo-format', 'How it works'],
  ['parser', 'How it works'],
  ['match-logic', 'How it works'],
  ['schema', 'How it works'],
  ['worker', 'How it works'],
  ['viewer', 'How it works'],
  ['testing', 'Running it'],
  ['infra', 'Running it'],
  ['maps', 'Operations'],
  ['r2-layout', 'Operations'],
  ['runbook-reparse', 'Operations'],
  ['runbook-stuck-matches', 'Operations'],
  ['decisions', 'Reference'],
  ['glossary', 'Reference'],
  ['changelog', 'Reference'],
]

// The first plain paragraph after the title, stripped of markdown, for list rows.
function describe(md: string): string {
  const body = md.replace(/^#\s+.+$/m, '')
  for (const block of body.split(/\n\s*\n/)) {
    const t = block.trim()
    if (!t || t.endsWith(':') || /^(#|\||```|<!--|[-*]\s|\d+\.\s|>)/.test(t)) continue
    const plain = t.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1').replace(/[`*_]/g, '').replace(/\s+/g, ' ')
    return plain.length > 170 ? `${plain.slice(0, plain.lastIndexOf(' ', 167))}...` : plain
  }
  return ''
}

function toPage(file: string, md: string): Page {
  const slug = file.replace(/^.*\//, '').replace(/\.md$/i, '').toLowerCase()
  const title = /^#\s+(.+)$/m.exec(md)?.[1].trim() ?? slug
  const words = md.split(/\s+/).length
  const group = PATH.find(([s]) => s === slug)?.[1] ?? 'More'
  return { slug, title, description: describe(md), md, minutes: Math.max(1, Math.round(words / 200)), group }
}

const rank = (slug: string) => {
  const i = PATH.findIndex(([s]) => s === slug)
  return i < 0 ? PATH.length : i
}

export const pages: Page[] = Object.entries(files)
  .map(([f, md]) => toPage(f, md))
  .sort((a, b) => rank(a.slug) - rank(b.slug) || a.slug.localeCompare(b.slug))

export const pageBySlug = (slug: string): Page | undefined => pages.find((p) => p.slug === slug)
export const groups = [...new Set(pages.map((p) => p.group))]

export interface Term {
  id: string
  term: string
  definition: string
}

// Glossary entries are `**Term.** Definition` lines.
export function glossaryTerms(): Term[] {
  const md = pageBySlug('glossary')?.md ?? ''
  return [...md.matchAll(/^\*\*(.+?)\.\*\* (.+)$/gm)].map((m) => ({ id: m[1], term: m[1], definition: m[2] }))
}

// The newest changelog section, for the home page.
export function latestChange(): string {
  const md = pageBySlug('changelog')?.md ?? ''
  const start = md.search(/^## /m)
  if (start < 0) return ''
  const next = md.slice(start + 3).search(/^## /m)
  return next < 0 ? md.slice(start) : md.slice(start, start + 3 + next)
}

// The date of the newest changelog entry, for the footer.
export function lastUpdated(): string {
  const m = /^## (\d{4}-\d{2}-\d{2})/m.exec(pageBySlug('changelog')?.md ?? '')
  if (!m) return ''
  return new Date(`${m[1]}T12:00:00`).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}
