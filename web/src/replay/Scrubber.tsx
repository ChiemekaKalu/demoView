// The timeline: drag (mouse or touch, via pointer events) or click to seek.
// Kill markers sit on the track.

import { useRef } from 'preact/hooks'
import { clamp } from '../lib/interp'
import { formatClock } from '../lib/timeline'
import type { Side } from '../lib/types'
import { SIDE_COLOR } from './draw'

export interface Marker {
  t: number
  side: Side
  label: string
}

interface Props {
  t: number
  duration: number
  markers: readonly Marker[]
  // Where the round ended, if known; the rest is shaded.
  endT: number | null
  onSeek: (t: number, done: boolean) => void
}

export function Scrubber({ t, duration, markers, endT, onSeek }: Props) {
  const trackRef = useRef<HTMLDivElement>(null)
  const draggingRef = useRef(false)
  const pct = (v: number) => `${duration > 0 ? clamp(v / duration, 0, 1) * 100 : 0}%`

  const timeAt = (clientX: number) => {
    const rect = trackRef.current!.getBoundingClientRect()
    return clamp((clientX - rect.left) / rect.width, 0, 1) * duration
  }
  const onDown = (e: PointerEvent) => {
    if (e.button !== 0) return
    draggingRef.current = true
    trackRef.current!.setPointerCapture(e.pointerId)
    onSeek(timeAt(e.clientX), false)
  }
  const onMove = (e: PointerEvent) => {
    if (draggingRef.current) onSeek(timeAt(e.clientX), false)
  }
  const onUp = (e: PointerEvent) => {
    if (!draggingRef.current) return
    draggingRef.current = false
    onSeek(timeAt(e.clientX), true)
  }
  const onCancel = () => {
    if (!draggingRef.current) return
    draggingRef.current = false
    onSeek(t, true)
  }
  const onKey = (e: KeyboardEvent) => {
    // Arrow keys are handled page-wide (5 s steps); Home and End jump.
    if (e.key === 'Home') onSeek(0, true)
    else if (e.key === 'End') onSeek(duration, true)
    else return
    e.preventDefault()
  }

  return (
    <div
      ref={trackRef}
      class="scrubber"
      role="slider"
      tabIndex={0}
      aria-label="Round time"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(t)}
      aria-valuetext={`${formatClock(t)} of ${formatClock(duration)}`}
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={onCancel}
      onKeyDown={onKey}
    >
      <div class="scrubber-track">
        <div class="scrubber-fill" style={{ width: pct(t) }} />
        {endT !== null && endT < duration && (
          <div class="scrubber-after" style={{ left: pct(endT) }} title="After the round ended" />
        )}
        {markers.map((m, i) => (
          <div
            key={i}
            class="scrubber-mark"
            style={{ left: pct(m.t), background: SIDE_COLOR[m.side] }}
            title={`${formatClock(m.t)} ${m.label}`}
            aria-hidden="true"
          />
        ))}
      </div>
      <div class="scrubber-thumb" style={{ left: pct(t) }} />
    </div>
  )
}
