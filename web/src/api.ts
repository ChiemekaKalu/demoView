// The data layer. API mode talks to the Worker (worker/README.md); fixtures
// mode serves schema/samples (see vite.config.ts) with no backend. Fixtures
// mode is on for every match when VITE_FIXTURES=1, and always for the match
// id "sample" (never a real id: those are 13 base32 characters).

import { deriveSummary, roundFileName } from './lib/fixtures'
import { schemaProblem } from './lib/errors'
import { apiMap, MAPS, type MapInfo, type RadarInfo } from './lib/maps'
import type { MatchFile, MatchSummary, RoundFile } from './lib/types'

const API_BASE = (import.meta.env.VITE_API_BASE ?? '').replace(/\/+$/, '')
const SAMPLES = `${import.meta.env.BASE_URL}samples`

export const FIXTURES = import.meta.env.VITE_FIXTURES === '1'
export const SAMPLE_ID = 'sample'
export const SAMPLE_ROUNDS: readonly number[] = __SAMPLE_ROUNDS__

export function isFixture(id: string): boolean {
  return FIXTURES || id === SAMPLE_ID
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(url, init)
  } catch (err) {
    if (isAbort(err)) throw err
    throw new ApiError('Could not reach the server.', 0)
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`.trim()
    try {
      const body: unknown = await res.json()
      if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') message = body.error
    } catch {
      // Not JSON; keep the status line.
    }
    throw new ApiError(message, res.status)
  }
  return (await res.json()) as T
}

const enc = encodeURIComponent

// A file in a data format this viewer doesn't read fails with a message
// saying how to fix it.
function readable<T extends { schema_version: number }>(file: T): T {
  const problem = schemaProblem(file.schema_version)
  if (problem) throw new Error(problem)
  return file
}

// ---- Reading -------------------------------------------------------------

const matchFiles = new Map<string, Promise<MatchFile | null>>()
const readySummaries = new Map<string, MatchSummary>()

// A reparse overwrites a match's R2 keys, but round files are served as
// immutable. Tagging parsed-file URLs with the ready summary's parsed_at gives
// each parse its own cache entry. The Worker ignores the query.
function version(id: string): string {
  const parsedAt = readySummaries.get(id)?.parsed_at
  return parsedAt ? `?v=${parsedAt}` : ''
}

// The full match.json: kills (with assisters and stable indexes) and names,
// from GET /api/matches/:id/match.json. A 404 (not ready, or no file) gives
// null, and the replay falls back to what the round file alone shows.
export function getMatchFile(id: string): Promise<MatchFile | null> {
  const key = isFixture(id) ? SAMPLE_ID : id
  let p = matchFiles.get(key)
  if (!p) {
    p = isFixture(id)
      ? request<MatchFile>(`${SAMPLES}/match.json`).then(readable)
      : request<MatchFile>(`${API_BASE}/api/matches/${enc(id)}/match.json${version(id)}`).then(readable, (err: unknown) => {
          if (err instanceof ApiError && err.status === 404) return null
          throw err
        })
    p.catch(() => matchFiles.delete(key))
    matchFiles.set(key, p)
  }
  return p
}

// GET /api/matches/:id. A ready summary is kept for the session; `fresh`
// skips that (the match page polls with it).
export async function getSummary(id: string, opts: { fresh?: boolean; signal?: AbortSignal } = {}): Promise<MatchSummary> {
  if (isFixture(id)) {
    const file = await getMatchFile(id)
    if (!file) throw new ApiError('sample match.json is missing', 404)
    return deriveSummary(file, id)
  }
  const cached = readySummaries.get(id)
  if (cached && !opts.fresh) return cached
  const summary = await request<MatchSummary>(`${API_BASE}/api/matches/${enc(id)}`, {
    cache: 'no-store',
    signal: opts.signal,
  })
  if (summary.status === 'ready') readySummaries.set(id, summary)
  return summary
}

// Whether round n has a replay file. Fixtures only have a few.
export function hasReplay(id: string, n: number, summary: MatchSummary): boolean {
  if (isFixture(id)) return SAMPLE_ROUNDS.includes(n)
  return summary.status === 'ready' && summary.rounds.some((r) => r.n === n)
}

// Parsed round files, most recently used last. A few are kept so stepping
// back and forth between rounds doesn't refetch.
const rounds = new Map<string, Promise<RoundFile | null>>()
const MAX_ROUNDS = 4

// One round file, or null when the round has none (404).
export function getRound(id: string, n: number, opts: { background?: boolean } = {}): Promise<RoundFile | null> {
  const fixture = isFixture(id)
  const key = `${fixture ? SAMPLE_ID : id}/${n}`
  let p = rounds.get(key)
  if (p) {
    rounds.delete(key)
    rounds.set(key, p)
    return p
  }
  if (fixture && !SAMPLE_ROUNDS.includes(n)) return Promise.resolve(null)
  const url = fixture
    ? `${SAMPLES}/rounds/${roundFileName(n)}`
    : `${API_BASE}/api/matches/${enc(id)}/rounds/${n}${version(id)}`
  p = request<RoundFile>(url, { priority: opts.background ? 'low' : 'auto' }).then(readable, (err: unknown) => {
    if (err instanceof ApiError && err.status === 404) return null
    throw err
  })
  p.catch(() => rounds.delete(key))
  rounds.set(key, p)
  while (rounds.size > MAX_ROUNDS) rounds.delete(rounds.keys().next().value!)
  return p
}

export function prefetchRound(id: string, n: number): void {
  getRound(id, n, { background: true }).catch(() => {})
}

const maps = new Map<string, Promise<MapInfo | null>>()

// A map's radar from GET /api/maps/:name, else the one bundled with the
// viewer, else null and the replay draws on a plain background. Only a 404 is
// kept for the session; another failure is tried again next time.
export function getMap(name: string): Promise<MapInfo | null> {
  let p = maps.get(name)
  if (!p) {
    const bundled = MAPS[name] ?? null
    const url = `${API_BASE}/api/maps/${enc(name)}`
    p = FIXTURES
      ? Promise.resolve(bundled)
      : request<RadarInfo>(url).then(
          (info) => apiMap(info, `${url}/radar.png`),
          (err: unknown) => {
            if (!(err instanceof ApiError && err.status === 404)) maps.delete(name)
            return bundled
          },
        )
    maps.set(name, p)
  }
  return p
}

// ---- Uploading -----------------------------------------------------------

export type UploadStage = 'creating' | 'uploading' | 'confirming'

export interface UploadCallbacks {
  onStage: (stage: UploadStage) => void
  onProgress: (loaded: number, total: number) => void
  signal: AbortSignal
}

// The whole upload flow. Resolves to the match id: a new match, or the one
// that already has this demo.
export async function uploadDemo(file: File, token: string, cb: UploadCallbacks): Promise<string> {
  if (FIXTURES) return simulateUpload(file, cb)
  const auth = { authorization: `Bearer ${token}` }
  cb.onStage('creating')
  const created = await request<{ id: string; upload_url: string; upload_expires_in: number }>(
    `${API_BASE}/api/matches`,
    { method: 'POST', headers: auth, signal: cb.signal },
  )
  cb.onStage('uploading')
  await putFile(created.upload_url, file, cb.onProgress, cb.signal)
  cb.onStage('confirming')
  const confirmed = await request<{ id: string }>(`${API_BASE}/api/matches/${enc(created.id)}/uploaded`, {
    method: 'POST',
    headers: auth,
    signal: cb.signal,
  })
  return confirmed.id
}

// PUT to the presigned R2 URL. XHR rather than fetch, for upload progress.
export function putFile(
  url: string,
  file: Blob,
  onProgress: (loaded: number, total: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(new DOMException('Upload cancelled', 'AbortError'))
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url)
    xhr.upload.onprogress = (e) => onProgress(e.loaded, e.lengthComputable ? e.total : file.size)
    xhr.onload = () =>
      xhr.status >= 200 && xhr.status < 300
        ? resolve()
        : reject(new ApiError(`The storage service refused the upload (${xhr.status}).`, xhr.status))
    xhr.onerror = () =>
      reject(new ApiError('The upload failed: a network error, or the bucket does not allow this origin (CORS).', 0))
    xhr.onabort = () => reject(new DOMException('Upload cancelled', 'AbortError'))
    signal?.addEventListener('abort', () => xhr.abort(), { once: true })
    // An untyped blob, so the browser sends no Content-Type header and the
    // bucket's CORS rule only has to allow PUT from this origin.
    xhr.send(file.slice(0, file.size, ''))
  })
}

// Fixtures mode has no backend: fake the progress, then open the sample.
function simulateUpload(file: File, cb: UploadCallbacks): Promise<string> {
  return new Promise((resolve, reject) => {
    cb.onStage('uploading')
    const total = file.size || 1
    const started = performance.now()
    const timer = setInterval(() => {
      const p = Math.min((performance.now() - started) / 1200, 1)
      cb.onProgress(Math.round(total * p), total)
      if (p >= 1) {
        clearInterval(timer)
        cb.onStage('confirming')
        resolve(SAMPLE_ID)
      }
    }, 50)
    cb.signal.addEventListener('abort', () => {
      clearInterval(timer)
      reject(new DOMException('Upload cancelled', 'AbortError'))
    })
  })
}
