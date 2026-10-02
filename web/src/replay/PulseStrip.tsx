// Match Pulse under the scrubber: the chance the CTs win the round, stepping
// at round start and after each kill. It spans the same width as the
// scrubber, so a kill mark and the step it caused line up.

import { useMemo } from 'preact/hooks'
import { clamp } from '../lib/interp'
import { oddsAt, percent, type PulsePoint } from '../lib/pulse'
import { formatClock } from '../lib/timeline'

interface Props {
  line: readonly PulsePoint[]
  t: number
  duration: number
  // When the biggest swing happened, to jump to.
  swingT: number | null
  // Drawn by the fallback model because Jev was unavailable.
  estimated: boolean
  onSeek: (t: number) => void
}

const W = 1000
const H = 40

// Seconds to the chart's x.
const xAt = (s: number, duration: number) => clamp(s / duration, 0, 1) * W

export function PulseStrip({ line, t, duration, swingT, estimated, onSeek }: Props) {
  const x = (s: number) => xAt(s, duration)
  const path = useMemo(() => {
    const y = (p: number) => (1 - p) * H
    let d = `M ${xAt(line[0]!.t, duration)} ${y(line[0]!.p)}`
    for (const pt of line.slice(1)) d += ` H ${xAt(pt.t, duration)} V ${y(pt.p)}`
    return `${d} H ${W}`
  }, [line, duration])
  const now = oddsAt(line, t)

  return (
    <div class="pulse">
      <div class="pulse-chart">
        <svg
          viewBox={`0 0 ${W} ${H}`}
          preserveAspectRatio="none"
          role="img"
          aria-label={now === null ? 'CT win chance' : `CT win chance ${percent(now)} at ${formatClock(t)}`}
        >
          <rect class="pulse-ct" width={W} height={H / 2} />
          <rect class="pulse-t" y={H / 2} width={W} height={H / 2} />
          <line class="pulse-mid" x2={W} y1={H / 2} y2={H / 2} vector-effect="non-scaling-stroke" />
          <path class="pulse-line" d={path} vector-effect="non-scaling-stroke" />
          <line class="pulse-now" x1={x(t)} x2={x(t)} y2={H} vector-effect="non-scaling-stroke" />
        </svg>
        {swingT !== null && (
          <button
            type="button"
            class="pulse-swing"
            style={{ left: `${(x(swingT) / W) * 100}%` }}
            onClick={() => onSeek(swingT)}
            title={`Biggest swing, at ${formatClock(swingT)}`}
            aria-label={`Jump to the biggest swing, at ${formatClock(swingT)}`}
          >
            ⚡
          </button>
        )}
      </div>
      <p class="pulse-legend muted small">
        CT win chance{now !== null && <strong> {percent(now)}</strong>}
        {estimated && ' · estimated: the AI model was unavailable for this match'}
      </p>
    </div>
  )
}
