import { useState } from 'preact/hooks'
import { radarUrl } from '../api'
import { galleryDate } from '../lib/format'
import { mapTitle } from '../lib/maps'
import type { GalleryMatch } from '../lib/types'
import { Link } from '../Link'

// Empty and error states still carry the sample card, so the grid is never bare.
export type GalleryState = { kind: 'loading' } | { kind: 'ready' | 'empty' | 'error'; matches: GalleryMatch[] }

const NOTE = { empty: 'No featured matches yet.', error: "Featured matches couldn't load." }

// Regulation is 24 rounds; more means overtime.
const REGULATION = 24

export function Gallery({ state }: { state: GalleryState }) {
  return (
    <section class="gallery" aria-labelledby="featured" aria-busy={state.kind === 'loading'}>
      <header class="gallery-head">
        <h2 id="featured">Featured matches</h2>
        {state.kind === 'ready' && (
          <span class="muted small">
            {state.matches.length} {state.matches.length === 1 ? 'match' : 'matches'}
          </span>
        )}
      </header>
      {(state.kind === 'empty' || state.kind === 'error') && <p class="muted">{NOTE[state.kind]}</p>}
      <ol class="gallery-grid">
        {state.kind === 'loading'
          ? [0, 1, 2].map((i) => <li key={i} class="card skeleton" aria-hidden="true" />)
          : state.matches.map((m) => (
              <li key={m.id}>
                <MatchCard m={m} />
              </li>
            ))}
      </ol>
    </section>
  )
}

// One featured match: map, score and date. Never names or Steam ids.
export function MatchCard({ m }: { m: GalleryMatch }) {
  const [broken, setBroken] = useState(false)
  const src = broken ? null : radarUrl(m.map)
  const title = mapTitle(m.map)
  const rounds = m.score_a !== null && m.score_b !== null ? m.score_a + m.score_b : null
  const date = m.parsed_at ? galleryDate(m.parsed_at) : null
  const label = [title, rounds !== null && `Team A ${m.score_a}, Team B ${m.score_b}`, date].filter(Boolean).join(', ')
  return (
    <Link class="card match-card" href={`/m/${m.id}`} aria-label={label}>
      <div class={src ? 'card-thumb' : 'card-thumb no-radar'}>
        {src ? (
          <img
            src={src}
            alt=""
            loading="lazy"
            decoding="async"
            width={1024}
            height={1024}
            onError={() => setBroken(true)}
          />
        ) : (
          title
        )}
      </div>
      <div class="card-body">
        <h3 class="card-map">{title}</h3>
        <Score a={m.score_a} b={m.score_b} />
        <p class="card-meta muted small">
          {date && <time dateTime={new Date(m.parsed_at).toISOString()}>{date}</time>}
          {date && rounds !== null && ' · '}
          {rounds !== null && `${rounds} rounds`}
          {rounds !== null && rounds > REGULATION && (
            <>
              {' '}
              <span class="pill">OT</span>
            </>
          )}
        </p>
      </div>
    </Link>
  )
}

// The winner's number stands out; a tie shows both alike.
function Score({ a, b }: { a: number | null; b: number | null }) {
  if (a === null || b === null) return <p class="card-score">–</p>
  return (
    <p class="card-score">
      <span class="lbl">A</span> <b class={a < b ? 'lose' : undefined}>{a}</b> –{' '}
      <b class={b < a ? 'lose' : undefined}>{b}</b> <span class="lbl">B</span>
    </p>
  )
}
