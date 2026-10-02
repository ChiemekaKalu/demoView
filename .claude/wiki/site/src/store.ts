// Everything personal lives in localStorage under one key. Reads and writes
// are wrapped because the accessor can throw (private windows, blocked site
// data); the suite then works for the session and forgets on reload.

export interface CardState {
  box: number
  due: number
}

export interface QuizResult {
  best: number
  total: number
  last: number
}

export type Theme = 'auto' | 'light' | 'dark'

interface State {
  learned: string[]
  notes: Record<string, string>
  quiz: Record<string, QuizResult>
  cards: Record<string, CardState>
  theme: Theme
}

const KEY = 'demoview-wiki'
const empty = (): State => ({ learned: [], notes: {}, quiz: {}, cards: {}, theme: 'auto' })

function load(): State {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) return { ...empty(), ...JSON.parse(raw) }
  } catch {
    // fall through to a fresh state
  }
  return empty()
}

let state = load()
const listeners = new Set<() => void>()

function save(): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(state))
  } catch {
    // keep going in memory
  }
  listeners.forEach((fn) => fn())
}

export const store = {
  get: (): Readonly<State> => state,
  subscribe(fn: () => void): void {
    listeners.add(fn)
  },
  isLearned: (slug: string): boolean => state.learned.includes(slug),
  toggleLearned(slug: string): void {
    state.learned = state.learned.includes(slug) ? state.learned.filter((s) => s !== slug) : [...state.learned, slug]
    save()
  },
  setNote(slug: string, text: string): void {
    if (text) state.notes[slug] = text
    else delete state.notes[slug]
    save()
  },
  recordQuiz(scope: string, score: number, total: number): void {
    const prev = state.quiz[scope]
    state.quiz[scope] = { best: Math.max(prev?.best ?? 0, score), total, last: score }
    save()
  },
  setCard(id: string, card: CardState): void {
    state.cards[id] = card
    save()
  },
  setTheme(theme: Theme): void {
    state.theme = theme
    save()
  },
  reset(): void {
    state = empty()
    save()
  },
}
