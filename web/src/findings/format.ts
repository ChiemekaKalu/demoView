// A win rate with its Wilson 95% interval, over n rounds (or duels, plants…).
export interface Rate {
  n: number
  p: number
  lo: number
  hi: number
}

export function pct(p: number): string {
  return `${Math.round(p * 100)}%`
}

export function interval(r: Rate): string {
  return `${Math.round(r.lo * 100)}–${Math.round(r.hi * 100)}%`
}

export function count(n: number): string {
  return n.toLocaleString('en-US')
}

// Hover text for a bar: the rate, its interval and its sample.
export function tip(r: Rate, unit: string): string {
  return `${pct(r.p)} (95% interval ${interval(r)}), ${count(r.n)} ${unit}`
}

const TITLES: Record<string, string> = { dust2: 'Dust2' }

export function mapTitle(id: string): string {
  return TITLES[id] ?? id.charAt(0).toUpperCase() + id.slice(1)
}

export function usd(x: number | null): string {
  if (x === null) return '—'
  if (x === 0) return 'free'
  return `$${x.toFixed(x >= 0.001 ? 4 : 5)}`
}
