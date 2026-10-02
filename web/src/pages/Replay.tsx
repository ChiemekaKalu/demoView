import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { getMap, getMatchFile, getRound, getSummary, hasReplay, isFixture, prefetchRound, SAMPLE_ROUNDS } from '../api'
import { reasonLabel, SIDE_GLYPH, sideOf } from '../lib/format'
import { fitMap, mapTitle, type MapInfo } from '../lib/maps'
import { SPEEDS } from '../lib/clock'
import { formatClock, parseT, roundDuration, roundKills, tickToFrame, urlWithT } from '../lib/timeline'
import type { MatchFile, MatchSummary, RoundFile, Side } from '../lib/types'
import { KillFeed } from '../replay/KillFeed'
import { Scrubber, type Marker } from '../replay/Scrubber'
import { loadRadar, Stage } from '../replay/stage'
import type { Scene } from '../replay/draw'
import { Link } from '../Link'
import { useTitle } from '../router'

type Load =
  | { kind: 'loading' }
  | { kind: 'error'; message: string }
  | { kind: 'not-ready'; summary: MatchSummary }
  | { kind: 'missing'; summary: MatchSummary; exists: boolean }
  | { kind: 'ready'; summary: MatchSummary; round: RoundFile; file: MatchFile | null; map: MapInfo | null }

export function Replay({ id, n }: { id: string; n: number }) {
  const [load, setLoad] = useState<Load>({ kind: 'loading' })
  const [attempt, setAttempt] = useState(0)
  useTitle(`Round ${n}`)

  useEffect(() => {
    let live = true
    setLoad({ kind: 'loading' })
    ;(async () => {
      const summary = await getSummary(id)
      if (summary.status !== 'ready') return { kind: 'not-ready', summary } as const
      if (!summary.rounds.some((r) => r.n === n)) return { kind: 'missing', summary, exists: false } as const
      // match.json is optional: without it the replay shows what the round
      // file alone can (victims, not attackers or weapons).
      const [round, file, map] = await Promise.all([
        getRound(id, n),
        getMatchFile(id).catch(() => null),
        summary.map ? getMap(summary.map) : null,
      ])
      if (!round) return { kind: 'missing', summary, exists: true } as const
      if (hasReplay(id, n + 1, summary)) prefetchRound(id, n + 1)
      return { kind: 'ready', summary, round, file, map } as const
    })().then(
      (next) => live && setLoad(next),
      (err: unknown) => live && setLoad({ kind: 'error', message: err instanceof Error ? err.message : String(err) }),
    )
    return () => {
      live = false
    }
  }, [id, n, attempt])

  const back = (
    <Link href={`/m/${id}`} class="back">
      ← Match
    </Link>
  )
  switch (load.kind) {
    case 'loading':
      return (
        <main class="page wide">
          {back}
          <p class="muted" role="status">
            Loading round {n}…
          </p>
        </main>
      )
    case 'error':
      return (
        <main class="page wide">
          {back}
          <h1>Round {n}</h1>
          <p class="error" role="alert">
            Could not load this round: {load.message}
          </p>
          <button type="button" onClick={() => setAttempt((a) => a + 1)}>
            Try again
          </button>
        </main>
      )
    case 'not-ready':
      return (
        <main class="page wide">
          {back}
          <h1>Round {n}</h1>
          <p>This match isn't ready yet ({load.summary.status}).</p>
        </main>
      )
    case 'missing':
      return (
        <main class="page wide">
          <RoundHeader id={id} n={n} summary={load.summary} />
          <p class="notice" role="status">
            {load.exists ? `Replay not available for round ${n}.` : `This match has no round ${n}.`}
            {load.exists && isFixture(id) && ` The samples only include rounds ${SAMPLE_ROUNDS.join(', ')}.`}
          </p>
        </main>
      )
    case 'ready':
      return (
        <main class="page wide">
          <RoundHeader id={id} n={n} summary={load.summary} />
          <ReplayView
            key={`${id}/${n}`}
            n={n}
            summary={load.summary}
            round={load.round}
            file={load.file}
            map={load.map}
          />
        </main>
      )
  }
}

function RoundHeader({ id, n, summary }: { id: string; n: number; summary: MatchSummary }) {
  const rounds = summary.rounds
  const i = rounds.findIndex((r) => r.n === n)
  const prev = i > 0 ? rounds[i - 1] : null
  const next = i >= 0 && i < rounds.length - 1 ? rounds[i + 1] : null
  const info = rounds[i]
  // Score going into this round.
  let a = 0
  let b = 0
  for (const r of rounds) {
    if (r.n >= n) break
    if (r.winner === 'A') a++
    else if (r.winner === 'B') b++
  }
  const sideA = sideOf('A', info?.side_a)
  const sideB = sideOf('B', info?.side_a)
  return (
    <header class="round-head">
      <nav class="round-nav" aria-label="Rounds">
        <Link href={`/m/${id}`} class="back">
          ← {mapTitle(summary.map)}
        </Link>
        <span class="spacer" />
        {prev ? (
          <Link href={`/m/${id}/r/${prev.n}`} class="btn" aria-label={`Previous round (${prev.n})`}>
            ‹ Prev
          </Link>
        ) : (
          <span class="btn disabled" aria-hidden="true">
            ‹ Prev
          </span>
        )}
        {next ? (
          <Link href={`/m/${id}/r/${next.n}`} class="btn" aria-label={`Next round (${next.n})`}>
            Next ›
          </Link>
        ) : (
          <span class="btn disabled" aria-hidden="true">
            Next ›
          </span>
        )}
      </nav>
      <h1>
        Round {n} <span class="muted">of {rounds.length}</span>
      </h1>
      <p class="round-score">
        <span class={`who ${sideA.toLowerCase()}`}>
          <span aria-hidden="true">{SIDE_GLYPH[sideA]}</span> Team A ({sideA})
        </span>{' '}
        <strong>
          {a} – {b}
        </strong>{' '}
        <span class={`who ${sideB.toLowerCase()}`}>
          Team B ({sideB}) <span aria-hidden="true">{SIDE_GLYPH[sideB]}</span>
        </span>
        {info?.winner && (
          <span class="muted">
            {' '}
            · won by Team {info.winner}, {reasonLabel(info.reason).toLowerCase()}
          </span>
        )}
      </p>
    </header>
  )
}

interface ViewProps {
  n: number
  summary: MatchSummary
  round: RoundFile
  file: MatchFile | null
  // Null draws the round on a plain background, framed to fit it.
  map: MapInfo | null
}

function ReplayView({ n, summary, round, file, map }: ViewProps) {
  const hz = round.hz
  const duration = roundDuration(round)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const stageRef = useRef<Stage | null>(null)
  // ?t= opens the round at that moment.
  const [t0] = useState(() => parseT(new URLSearchParams(location.search).get('t'), duration))
  const [frame, setFrame] = useState(() => Math.floor(t0 * hz + 1e-6))
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(1)

  const model = useMemo(() => {
    const sideA = summary.rounds.find((r) => r.n === n)?.side_a ?? file?.rounds.find((r) => r.n === n)?.side_a
    const names = new Map<number, string>()
    const sides = new Map<number, Side>()
    for (const p of summary.players) {
      names.set(p.idx, p.name ?? `Player ${p.idx}`)
      sides.set(p.idx, sideOf(p.team, sideA))
    }
    for (const p of file?.players ?? []) {
      if (!names.has(p.idx)) names.set(p.idx, p.name)
      if (!sides.has(p.idx)) sides.set(p.idx, sideOf(p.team, sideA))
    }
    const kills = roundKills(round, file)
    const scene: Scene = { round, map: map ?? fitMap(round), names, sides, kills }
    const name = (idx: number | null) => (idx === null ? 'Unknown' : (names.get(idx) ?? `Player ${idx}`))
    const side = (idx: number | null) => (idx === null ? null : (sides.get(idx) ?? null))
    const markers: Marker[] = kills.map((k) => ({
      t: k.frame / hz,
      // The killer's side, or the side opposing the victim.
      side: (k.attacker !== null ? side(k.attacker) : null) ?? (side(k.victim) === 'CT' ? 'T' : 'CT'),
      label: k.attacker !== null ? `${name(k.attacker)} killed ${name(k.victim)}` : `${name(k.victim)} died`,
    }))
    const info = file?.rounds.find((r) => r.n === n)
    const endT = info && file ? tickToFrame(info.end_tick, round.start_tick, file.match.tick_rate, hz) / hz : null
    return { scene, kills, name, side, markers, endT }
  }, [summary, round, file, map, n, hz])

  // The stage lives as long as this view.
  useEffect(() => {
    const s = new Stage(canvasRef.current!, model.scene, duration, t0)
    stageRef.current = s
    // The canvas draws every display frame by itself. While playing, the text
    // around it (clock, scrubber, kill feed) only needs ten updates a second.
    let shown = { tenth: -1, playing: false }
    s.onUpdate = (t, isPlaying, ended) => {
      const tenth = Math.floor(t * 10)
      if (isPlaying && shown.playing && tenth === shown.tenth && !ended) return
      shown = { tenth, playing: isPlaying }
      setFrame(Math.floor(t * hz + 1e-6))
      setPlaying(isPlaying)
      if (ended) writeT(t)
    }
    const radar = model.scene.map.radar
    if (radar) loadRadar(radar).then((img) => img && s.setRadar(img))
    return () => s.destroy()
    // All stable: the view is keyed by round, so this runs once per round.
  }, [model, duration, t0, hz])

  const toggle = () => {
    const s = stageRef.current!
    s.toggle()
    if (!s.clock.playing) writeT(s.clock.t)
  }
  const seek = (t: number, done = true) => {
    const s = stageRef.current!
    s.seek(t)
    if (done) writeT(s.clock.t)
  }
  const step = (seconds: number) => seek(stageRef.current!.clock.t + seconds)
  const changeSpeed = (v: number) => {
    stageRef.current!.setSpeed(v)
    setSpeed(v)
  }

  // Space toggles play; the arrow keys step 5 s.
  useEffect(() => {
    const ignore = (e: KeyboardEvent) => {
      if (e.altKey || e.ctrlKey || e.metaKey) return true
      const el = e.target as HTMLElement | null
      return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))
    }
    const onDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || ignore(e)) return
      if (e.key === ' ') {
        e.preventDefault()
        if (!e.repeat) toggle()
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault()
        step(e.key === 'ArrowLeft' ? -5 : 5)
      }
    }
    // Stops a focused button from also "clicking" on Space.
    const onUp = (e: KeyboardEvent) => {
      if (e.key === ' ' && !ignore(e)) e.preventDefault()
    }
    addEventListener('keydown', onDown)
    addEventListener('keyup', onUp)
    return () => {
      removeEventListener('keydown', onDown)
      removeEventListener('keyup', onUp)
    }
  }, [])

  const t = frame / hz
  const alive = { CT: 0, T: 0 }
  for (const p of round.players) {
    if ((p.hp[Math.min(frame, p.hp.length - 1)] ?? 0) > 0) alive[model.side(p.idx) ?? 'CT']++
  }

  return (
    <div class="replay">
      <div class="replay-main">
        <div class="stage">
          <canvas
            ref={canvasRef}
            role="img"
            aria-label={`2D replay of round ${n}: ${alive.CT} CT and ${alive.T} T alive at ${formatClock(t)}`}
          />
        </div>
        <Scrubber t={t} duration={duration} markers={model.markers} endT={model.endT} onSeek={seek} />
        <div class="controls">
          <button type="button" class="play" onClick={toggle} aria-keyshortcuts="Space">
            {playing ? 'Pause' : 'Play'}
          </button>
          <button type="button" onClick={() => step(-5)} aria-keyshortcuts="ArrowLeft">
            −5 s
          </button>
          <button type="button" onClick={() => step(5)} aria-keyshortcuts="ArrowRight">
            +5 s
          </button>
          <span class="clock" aria-hidden="true">
            {formatClock(t)} / {formatClock(duration)}
          </span>
          <span class="spacer" />
          <div class="speeds" role="group" aria-label="Playback speed">
            {SPEEDS.map((v) => (
              <button type="button" key={v} aria-pressed={speed === v} onClick={() => changeSpeed(v)}>
                {v}×
              </button>
            ))}
          </div>
        </div>
        <p class="muted small hint">
          <kbd>Space</kbd> play or pause · <kbd>←</kbd> <kbd>→</kbd> 5 s · alive:{' '}
          <span class="who ct">
            {SIDE_GLYPH.CT} CT {alive.CT}
          </span>{' '}
          <span class="who t">
            {SIDE_GLYPH.T} T {alive.T}
          </span>
        </p>
      </div>
      <KillFeed
        kills={model.kills}
        frame={frame}
        hz={hz}
        name={model.name}
        side={model.side}
        partial={!file}
        onSeek={(v) => seek(v)}
      />
    </div>
  )
}

// Puts the moment in the URL (?t=) so it can be shared.
function writeT(t: number): void {
  history.replaceState(history.state, '', urlWithT(location.href, t))
}
