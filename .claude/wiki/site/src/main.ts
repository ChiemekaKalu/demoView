import '@fontsource-variable/newsreader/opsz.css'
import '@fontsource-variable/newsreader/opsz-italic.css'
import '@fontsource/ibm-plex-sans/latin-400.css'
import '@fontsource/ibm-plex-sans/latin-500.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import './style.css'
import { $, h } from './dom'
import { lastUpdated, pages } from './content'
import { store, type Theme } from './store'
import { renderCards } from './views/cards'
import { renderLab } from './views/lab'
import { renderMap } from './views/map'
import { renderPage } from './views/page'
import { renderPages } from './views/pages'
import { renderPicker, renderQuiz } from './views/quiz'
import { renderStudy } from './views/study'
import { renderLanding, renderStep } from './views/tour'
import { STEPS } from './tour'

const page = $<HTMLElement>('#page')!
const tabs = $<HTMLElement>('#tabs')!
const themeBtn = $<HTMLButtonElement>('#theme')!
const progress = $<HTMLElement>('#progress')!
const bar = $<HTMLElement>('#progress-bar')!
const root = document.documentElement

// Theme: the saved choice, else the system's. The toggle always saves an explicit one.
const systemDark = matchMedia('(prefers-color-scheme: dark)')
const resolved = (t: Theme): 'light' | 'dark' => (t === 'auto' ? (systemDark.matches ? 'dark' : 'light') : t)
const previewTheme = ['light', 'dark'].includes(new URLSearchParams(location.search).get('theme') ?? '')
function applyTheme() {
  if (previewTheme) return
  const next = resolved(store.get().theme)
  if (root.dataset.theme === next) return
  root.dataset.theme = next
  route()
}
themeBtn.addEventListener('click', () => {
  store.setTheme(root.dataset.theme === 'dark' ? 'light' : 'dark')
  applyTheme()
})
systemDark.addEventListener('change', () => store.get().theme === 'auto' && applyTheme())

type View = 'tour' | 'step' | 'map' | 'lab' | 'pages' | 'page' | 'study' | 'cards' | 'quiz'

interface Route {
  view: View
  arg: string
  frag: string
}

const TABS: { label: string; href: string; views: View[] }[] = [
  { label: 'Tour', href: '#/', views: ['tour', 'step'] },
  { label: 'Map', href: '#/map', views: ['map'] },
  { label: 'Lab', href: '#/lab', views: ['lab'] },
  { label: 'Pages', href: '#/pages', views: ['pages', 'page'] },
  { label: 'Study', href: '#/study', views: ['study', 'cards', 'quiz'] },
]

function parse(): Route {
  const [, view = '', arg = '', frag = ''] = location.hash.replace(/^#/, '').split('/')
  switch (view) {
    case 'tour': return arg ? { view: 'step', arg, frag: '' } : { view: 'tour', arg: '', frag: '' }
    case 'map': return { view: 'map', arg, frag: '' }
    case 'lab': return { view: 'lab', arg: '', frag: '' }
    case 'pages': return { view: 'pages', arg: '', frag: '' }
    case 'p': return { view: 'page', arg, frag }
    case 'study': return { view: 'study', arg: '', frag: '' }
    case 'cards': return { view: 'cards', arg: '', frag: '' }
    case 'quiz': return { view: 'quiz', arg, frag: '' }
    default: return { view: 'tour', arg: '', frag: '' }
  }
}

function drawTabs(r: Route) {
  tabs.replaceChildren(...TABS.map((t) => {
    const on = t.views.includes(r.view)
    return h('a', { href: t.href, class: on ? 'current' : '', 'aria-current': on ? 'page' : null }, t.label)
  }))
}

// A thin reading-progress bar under the header while a page is open.
function updateProgress() {
  const max = document.documentElement.scrollHeight - innerHeight
  bar.style.width = `${max > 0 ? Math.min(100, (scrollY / max) * 100) : 0}%`
}
addEventListener('scroll', updateProgress, { passive: true })

function route() {
  const r = parse()
  document.body.dataset.col = r.view === 'page' ? 'narrow' : 'standard'
  const reading = r.view === 'page'
  progress.hidden = !reading
  switch (r.view) {
    case 'tour': renderLanding(page); break
    case 'step': renderStep(page, Number(r.arg) || 1); break
    case 'map': renderMap(page, r.arg); break
    case 'lab': renderLab(page); break
    case 'pages': renderPages(page); break
    case 'page': renderPage(page, r.arg, r.frag); break
    case 'study': renderStudy(page); break
    case 'cards': renderCards(page); break
    case 'quiz': r.arg ? renderQuiz(page, r.arg) : renderPicker(page); break
  }
  if (r.view !== 'page') scrollTo(0, 0)
  const label = r.view === 'page' ? pages.find((p) => p.slug === r.arg)?.title : r.view === 'step' ? STEPS[(Number(r.arg) || 1) - 1]?.title : null
  document.title = label ? `${label} · demoView wiki` : 'demoView wiki'
  drawTabs(r)
  updateProgress()
}

addEventListener('hashchange', route)
$<HTMLElement>('#updated')!.textContent = lastUpdated() ? `Updated ${lastUpdated()}` : ''
applyTheme()
route()
