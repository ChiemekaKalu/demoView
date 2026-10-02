import { useEffect, useState } from 'preact/hooks'
import { ApiError, getSummary, hasReplay, isAbort, isFixture, SAMPLE_ROUNDS } from '../api'
import { explainFailure, schemaProblem } from '../lib/errors'
import { reasonLabel, reasonShort, SIDE_GLYPH, sideOf } from '../lib/format'
import { mapTitle } from '../lib/maps'
import type { MatchSummary, SummaryPlayer } from '../lib/types'
import { Link } from '../Link'
import { useTitle } from '../router'

const POLL_MS = 3000

const STATUS_TEXT: Record<string, string> = {
  uploaded: 'Waiting for the upload to be confirmed.',
  queued: 'Queued for parsing.',
  parsing: 'Parsing the demo. This usually takes under a minute.',
}

export function Match({ id }: { id: string }) {
  const [summary, setSummary] = useState<MatchSummary | null>(null)
  const [problem, setProblem] = useState<{ message: string; fatal: boolean } | null>(null)
  useTitle(summary?.status === 'ready' ? mapTitle(summary.map) : 'Match')

  // Polls until the match is ready or failed. Network errors keep polling.
  useEffect(() => {
    const ctrl = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    setSummary(null)
    setProblem(null)
    const poll = async () => {
      try {
        const s = await getSummary(id, { fresh: true, signal: ctrl.signal })
        setSummary(s)
        setProblem(null)
        if (s.status === 'ready' || s.status === 'failed') return
      } catch (err) {
        if (isAbort(err)) return
        if (err instanceof ApiError && err.status === 404) {
          setProblem({ message: 'No match with this id. Check the link.', fatal: true })
          return
        }
        setProblem({ message: `${err instanceof Error ? err.message : String(err)} Retrying…`, fatal: false })
      }
      timer = setTimeout(poll, POLL_MS)
    }
    poll()
    return () => {
      ctrl.abort()
      clearTimeout(timer)
    }
  }, [id])

  if (problem?.fatal) {
    return (
      <main class="page">
        <Link href="/" class="back">
          ← Upload
        </Link>
        <h1>Match not found</h1>
        <p class="error" role="alert">
          {problem.message}
        </p>
      </main>
    )
  }

  if (!summary || (summary.status !== 'ready' && summary.status !== 'failed')) {
    const status = summary?.status
    return (
      <main class="page">
        <Link href="/" class="back">
          ← Upload
        </Link>
        <h1>Preparing the match</h1>
        <div role="status" aria-live="polite">
          <p>{status ? STATUS_TEXT[status] ?? status : 'Checking the match…'}</p>
          {status === 'queued' && summary?.error && (
            <p class="muted">The last attempt failed ({summary.error}). It will be retried.</p>
          )}
          {problem && <p class="error">{problem.message}</p>}
        </div>
        <progress aria-label="Parse progress" />
        <p class="muted small">This page updates by itself. The link already works, so you can share it now.</p>
      </main>
    )
  }

  if (summary.status === 'failed') {
    const { message, detail } = explainFailure(summary.error)
    return (
      <main class="page">
        <Link href="/" class="back">
          ← Upload
        </Link>
        <h1>The parse failed</h1>
        <p class="error" role="alert">
          {message}
        </p>
        {detail && <p class="muted small">Details: {detail}</p>}
      </main>
    )
  }

  const startSide = sideOf('A', summary.rounds[0]?.side_a)
  const unreadable = schemaProblem(summary.schema_version)
  return (
    <main class="page">
      <Link href="/" class="back">
        ← Upload
      </Link>
      <header class="match-head">
        <h1>{mapTitle(summary.map)}</h1>
        <p class="final" aria-label={`Final score: Team A ${summary.score_a ?? 0}, Team B ${summary.score_b ?? 0}`}>
          <span>Team A</span>
          <strong>
            {summary.score_a ?? 0} – {summary.score_b ?? 0}
          </strong>
          <span>Team B</span>
        </p>
        {isFixture(id) && <p class="muted small">Sample data: a synthetic match from schema/samples.</p>}
      </header>
      {unreadable && (
        <p class="notice" role="status">
          {unreadable}
        </p>
      )}

      <section aria-labelledby="scoreboard">
        <h2 id="scoreboard">Scoreboard</h2>
        <div class="boards">
          <Board team="A" summary={summary} startSide={startSide} />
          <Board team="B" summary={summary} startSide={startSide === 'CT' ? 'T' : 'CT'} />
        </div>
      </section>

      <section aria-labelledby="rounds">
        <h2 id="rounds">Rounds</h2>
        <RoundList id={id} summary={summary} />
      </section>
    </main>
  )
}

function Board({ team, summary, startSide }: { team: 'A' | 'B'; summary: MatchSummary; startSide: 'CT' | 'T' }) {
  const rows = summary.players
    .filter((p) => p.team === team)
    .sort((a, b) => (b.kills ?? 0) - (a.kills ?? 0) || (a.deaths ?? 0) - (b.deaths ?? 0))
  const score = team === 'A' ? summary.score_a : summary.score_b
  return (
    <div class="board">
      <table>
        <caption>
          <span class={`who ${startSide.toLowerCase()}`}>
            <span aria-hidden="true">{SIDE_GLYPH[startSide]} </span>Team {team}
          </span>{' '}
          <span class="muted">
            · {score ?? 0} rounds · started {startSide}
          </span>
        </caption>
        <thead>
          <tr>
            <th scope="col">Player</th>
            <th scope="col">
              <abbr title="Kills">K</abbr>
            </th>
            <th scope="col">
              <abbr title="Deaths">D</abbr>
            </th>
            <th scope="col">
              <abbr title="Assists">A</abbr>
            </th>
            <th scope="col">
              <abbr title="Average damage per round">ADR</abbr>
            </th>
            <th scope="col">
              <abbr title="Headshot kills as a share of kills">HS%</abbr>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => (
            <Row key={p.idx} p={p} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Row({ p }: { p: SummaryPlayer }) {
  const num = (v: number | null, digits = 0) => (v === null ? '–' : v.toFixed(digits))
  return (
    <tr>
      <th scope="row">{p.name ?? `Player ${p.idx}`}</th>
      <td>{num(p.kills)}</td>
      <td>{num(p.deaths)}</td>
      <td>{num(p.assists)}</td>
      <td>{num(p.adr, 1)}</td>
      <td>{p.hs_pct === null ? '–' : `${Math.round(p.hs_pct)}%`}</td>
    </tr>
  )
}

function RoundList({ id, summary }: { id: string; summary: MatchSummary }) {
  const missing = summary.rounds.filter((r) => !hasReplay(id, r.n, summary)).length
  return (
    <>
      {missing > 0 && (
        <p class="muted small">
          Dimmed rounds have no replay file
          {isFixture(id) ? `: the samples include rounds ${SAMPLE_ROUNDS.join(', ')}.` : '.'}
        </p>
      )}
      <ol class="rounds">
        {summary.rounds.map((r, i) => {
          // A gap where sides swap (halftime, overtime halves).
          const swap = i > 0 && summary.rounds[i - 1].side_a !== r.side_a
          const side = r.winner ? sideOf(r.winner, r.side_a) : null
          const label = `Round ${r.n}: ${r.winner ? `Team ${r.winner} won as ${side}` : 'no winner'}, ${reasonLabel(r.reason).toLowerCase()}`
          const replay = hasReplay(id, r.n, summary)
          const body = (
            <>
              <span class="sr">
                {label}
                {replay ? '' : '. Replay not available.'}
              </span>
              <span class="round-n" aria-hidden="true">
                {r.n}
              </span>
              <span class={`round-win ${side ? side.toLowerCase() : ''}`} aria-hidden="true">
                {side ? SIDE_GLYPH[side] : ''} {r.winner ?? '?'}
              </span>
              <span class="round-reason" aria-hidden="true">
                {reasonShort(r.reason)}
              </span>
            </>
          )
          return (
            <li key={r.n} class={swap ? 'swap' : undefined}>
              {replay ? (
                <Link href={`/m/${id}/r/${r.n}`} class="round">
                  {body}
                </Link>
              ) : (
                <span class="round no-replay" title="Replay not available">
                  {body}
                </span>
              )}
            </li>
          )
        })}
      </ol>
    </>
  )
}
