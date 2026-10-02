// The round's kills, in sync with playback: kills that have happened are
// shown normally, the latest is highlighted, and later ones are dimmed.
// Clicking one seeks to just before it.

import { SIDE_GLYPH, weaponLabel } from '../lib/format'
import { formatClock, type FeedKill } from '../lib/timeline'
import type { Side } from '../lib/types'

const LEAD_IN = 2 // seconds before the kill

interface Props {
  kills: readonly FeedKill[]
  frame: number
  hz: number
  name: (idx: number | null) => string
  side: (idx: number | null) => Side | null
  // True when match.json was unavailable, so attackers are unknown.
  partial: boolean
  onSeek: (t: number) => void
}

export function KillFeed({ kills, frame, hz, name, side, partial, onSeek }: Props) {
  let latest = -1
  kills.forEach((k, i) => {
    if (k.frame <= frame) latest = i
  })
  return (
    <section class="feed" aria-label="Kill feed">
      <h2>Kills</h2>
      {kills.length === 0 && <p class="muted">No kills this round.</p>}
      <ol>
        {kills.map((k, i) => {
          const state = i === latest ? 'latest' : k.frame <= frame ? 'past' : 'future'
          const t = k.frame / hz
          return (
            <li key={i} class={`feed-item ${state}`}>
              <button
                type="button"
                onClick={() => onSeek(Math.max(0, t - LEAD_IN))}
                title={`Jump to ${LEAD_IN} s before this kill`}
              >
                <span class="feed-time">{formatClock(t)}</span>
                {k.attacker !== null ? (
                  <Who name={name(k.attacker)} side={side(k.attacker)} />
                ) : (
                  !partial && <span class="muted">World</span>
                )}
                <span class="feed-weapon">
                  {partial ? 'killed' : weaponLabel(k.weapon)}
                  {k.headshot && <abbr title="Headshot"> HS</abbr>}
                </span>
                <Who name={name(k.victim)} side={side(k.victim)} />
              </button>
            </li>
          )
        })}
      </ol>
      {partial && kills.length > 0 && <p class="muted small">Attackers and weapons need the match file.</p>}
    </section>
  )
}

function Who({ name, side }: { name: string; side: Side | null }) {
  return (
    <span class={`who ${side ? side.toLowerCase() : ''}`}>
      {side && <span aria-hidden="true">{SIDE_GLYPH[side]} </span>}
      {name}
    </span>
  )
}
