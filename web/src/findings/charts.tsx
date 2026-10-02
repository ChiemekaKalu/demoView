// Plain HTML and CSS charts for the findings page. Each bar carries its value
// as text and its interval and sample as hover text.

import type { ComponentChildren } from 'preact'
import { pct, tip, type Rate } from './format'

type Side = 'ct' | 't'

function width(p: number): string {
  return `${(p * 100).toFixed(1)}%`
}

function Interval({ r, vertical = false }: { r: Rate; vertical?: boolean }) {
  const style = vertical
    ? { bottom: width(r.lo), height: width(r.hi - r.lo) }
    : { left: width(r.lo), width: width(r.hi - r.lo) }
  return <span class={vertical ? 'fx-ci fx-ci-v' : 'fx-ci'} style={style} />
}

export function SideKey() {
  return (
    <p class="fx-key small muted">
      <span class="fx-swatch ct" /> CT <span class="fx-swatch t" /> T
    </p>
  )
}

// One bar per row, split at the CT share: CT fills from the left, T is the
// rest. The dashed line marks an even 50%.
export function SplitBars({ rows, unit }: { rows: { label: string; rate: Rate }[]; unit: string }) {
  return (
    <ul class="fx-rows fx-split">
      {rows.map(({ label, rate }) => (
        <li key={label} title={tip(rate, unit)}>
          <span class="fx-label">{label}</span>
          <span class="fx-num">{pct(rate.p)}</span>
          <span class="fx-track even">
            <span class="fx-fill ct" style={{ width: width(rate.p) }} />
            <Interval r={rate} />
          </span>
          <span class="fx-num">{pct(1 - rate.p)}</span>
        </li>
      ))}
    </ul>
  )
}

export interface BarRow {
  key: string
  label: ComponentChildren
  rate: Rate
  side: Side
}

export function RateBars({ rows, unit }: { rows: BarRow[]; unit: string }) {
  return (
    <ul class="fx-rows fx-rate">
      {rows.map(({ key, label, rate, side }) => (
        <li key={key} title={tip(rate, unit)}>
          <span class="fx-label">{label}</span>
          <span class="fx-track">
            <span class={`fx-fill ${side}`} style={{ width: width(rate.p) }} />
            <Interval r={rate} />
          </span>
          <span class="fx-num">{pct(rate.p)}</span>
        </li>
      ))}
    </ul>
  )
}

export function Columns({ cols, side, unit }: { cols: { label: string; rate: Rate }[]; side: Side; unit: string }) {
  return (
    <ol class="fx-cols">
      {cols.map(({ label, rate }) => (
        <li key={label} title={tip(rate, unit)}>
          <span class="fx-col-plot">
            <span class={`fx-col-bar ${side}`} style={{ height: width(rate.p) }} />
            <Interval r={rate} vertical />
            <span class="fx-col-num" style={{ bottom: width(rate.hi) }}>
              {pct(rate.p)}
            </span>
          </span>
          <span class="fx-col-label">{label}</span>
        </li>
      ))}
    </ol>
  )
}

export interface DumbbellRow {
  key: string
  label: string
  from: number
  to: number
}

// Two scores per row on a shared scale, joined by a line: `from` hollow,
// `to` filled.
export function Dumbbell({ rows, domain, ticks }: { rows: DumbbellRow[]; domain: [number, number]; ticks: number[] }) {
  const [lo, hi] = domain
  const x = (v: number) => ((v - lo) / (hi - lo)) * 100
  return (
    <div class="fx-dumbbell">
      <div class="fx-axis small muted" aria-hidden="true">
        <span />
        <span class="fx-track bare">
          {ticks.map((t) => (
            <span key={t} style={{ left: `${x(t)}%` }}>
              {t.toFixed(2)}
            </span>
          ))}
        </span>
        <span />
      </div>
      <ul class="fx-rows">
        {rows.map(({ key, label, from, to }) => (
          <li key={key}>
            <span class="fx-label">{label}</span>
            <span class="fx-track bare">
              <span class="fx-link" style={{ left: `${x(Math.min(from, to))}%`, width: `${Math.abs(x(from) - x(to))}%` }} />
              <span class="fx-dot from" style={{ left: `${x(from)}%` }} />
              <span class="fx-dot to" style={{ left: `${x(to)}%` }} />
            </span>
            <span class="fx-num wide">
              {from.toFixed(3)} → {to.toFixed(3)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
