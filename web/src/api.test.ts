import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  featureMatch,
  getEvents,
  getGallery,
  getMap,
  getMatchFile,
  getPulse,
  getRound,
  getSummary,
  radarUrl,
  uploadDemo,
} from './api'
import { MAPS } from './lib/maps'

// The module caches per match id, so each test uses its own id.
let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
})

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

const summary = (id: string, status: string, parseId: string | null = null) => ({
  id,
  status,
  error: null,
  map: 'de_mirage',
  score_a: 13,
  score_b: 9,
  parser_version: '0.1.0',
  schema_version: 1,
  created_at: 1,
  parsed_at: parseId ? 5 : null,
  parse_id: parseId,
  players: [],
  rounds: [{ n: 1, winner: 'A', reason: 'elimination', side_a: 'CT' }],
})

const urls = () => fetchMock.mock.calls.map(([url]) => String(url))

const PARSE = '01k6gq3m1abcdefg'

describe('getSummary', () => {
  it('keeps a ready summary for the session unless asked for a fresh one', async () => {
    const id = 'aaaaaaaaaaaa1'
    fetchMock.mockImplementation(async () => json(summary(id, 'ready', PARSE)))
    await getSummary(id)
    await getSummary(id)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await getSummary(id, { fresh: true })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does not cache a summary that is not ready yet', async () => {
    const id = 'aaaaaaaaaaaa2'
    fetchMock.mockImplementation(async () => json(summary(id, 'parsing')))
    await getSummary(id)
    await getSummary(id)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("surfaces the API's error message", async () => {
    fetchMock.mockResolvedValue(json({ error: 'match not found' }, 404))
    await expect(getSummary('aaaaaaaaaaaa3')).rejects.toEqual(new ApiError('match not found', 404))
  })
})

describe('parsed-file URLs', () => {
  it("name the ready summary's parse, so a reparse is never served stale", async () => {
    const id = 'aaaaaaaaaaaa4'
    fetchMock.mockImplementation(async (url: string) =>
      String(url).endsWith(`/api/matches/${id}`) ? json(summary(id, 'ready', PARSE)) : json({ schema_version: 1 }),
    )
    await getSummary(id)
    await getRound(id, 1)
    await getMatchFile(id)
    expect(urls()).toContain(`/api/matches/${id}/rounds/1?v=${PARSE}`)
    expect(urls()).toContain(`/api/matches/${id}/match.json?v=${PARSE}`)
  })

  it('follow a reparse once its old parse is gone', async () => {
    const id = 'aaaaaaaaaaab2'
    const NEWER = '01k6gq3m1newnewn'
    let live = PARSE
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).endsWith(`/api/matches/${id}`)) return json(summary(id, 'ready', live))
      return String(url).endsWith(`?v=${NEWER}`) ? json({ schema_version: 1, round: 3 }) : json({}, 404)
    })
    await getSummary(id)
    live = NEWER
    expect(await getRound(id, 3)).toEqual({ schema_version: 1, round: 3 })
    expect(urls().slice(1)).toEqual([
      `/api/matches/${id}/rounds/3?v=${PARSE}`,
      `/api/matches/${id}`,
      `/api/matches/${id}/rounds/3?v=${NEWER}`,
    ])
  })

  it('read the live parse for a match parsed before parse ids', async () => {
    const id = 'aaaaaaaaaaab3'
    fetchMock.mockImplementation(async (url: string) =>
      String(url).endsWith(`/api/matches/${id}`) ? json(summary(id, 'ready')) : json({ schema_version: 1 }),
    )
    await getSummary(id)
    await getRound(id, 1)
    expect(urls()).toContain(`/api/matches/${id}/rounds/1`)
  })
})

describe('getRound and getMatchFile', () => {
  it('give null on a 404 instead of failing', async () => {
    fetchMock.mockResolvedValue(json({ error: 'round not found' }, 404))
    expect(await getRound('aaaaaaaaaaaa5', 3)).toBeNull()
    expect(await getMatchFile('aaaaaaaaaaaa5')).toBeNull()
  })

  it('fetch each round once', async () => {
    fetchMock.mockImplementation(async () => json({ schema_version: 1, round: 2 }))
    await getRound('aaaaaaaaaaaa6', 2)
    await getRound('aaaaaaaaaaaa6', 2)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('reject a file in a data format this viewer does not read', async () => {
    fetchMock.mockImplementation(async () => json({ schema_version: 2, round: 2 }))
    await expect(getRound('aaaaaaaaaaab1', 2)).rejects.toThrow(/format 2.*Reload/)
  })

  it('retry a round whose fetch failed', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('offline')).mockImplementation(async () => json({ schema_version: 1, round: 2 }))
    await expect(getRound('aaaaaaaaaaaa7', 2)).rejects.toThrow('Could not reach the server.')
    expect(await getRound('aaaaaaaaaaaa7', 2)).toEqual({ schema_version: 1, round: 2 })
  })
})

describe('getMap', () => {
  it('builds the map from the API, radar URL included', async () => {
    fetchMock.mockResolvedValue(json({ name: 'de_inferno', pos_x: -2087, pos_y: 3870, scale: 4.9 }))
    expect(await getMap('de_inferno')).toEqual({
      title: 'Inferno',
      posX: -2087,
      posY: 3870,
      scale: 4.9,
      size: 1024,
      radar: '/api/maps/de_inferno/radar.png',
    })
  })

  it('marks a two-floor map, whose floors share its main radar', async () => {
    fetchMock.mockResolvedValue(json({ name: 'de_vertigo', pos_x: -3168, pos_y: 1762, scale: 4, two_floor: true }))
    expect(await getMap('de_vertigo')).toMatchObject({ title: 'Vertigo', twoFloor: true })
  })

  it('falls back to the bundled radar, and keeps a 404 for the session', async () => {
    fetchMock.mockResolvedValue(json({ error: 'no radar for this map' }, 404))
    expect(await getMap('de_mirage')).toBe(MAPS.de_mirage)
    expect(await getMap('de_nuke')).toBeNull()
    expect(await getMap('de_nuke')).toBeNull()
    expect(urls().filter((u) => u.endsWith('/de_nuke'))).toHaveLength(1)
  })

  it('tries again after a network failure', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('offline')).mockResolvedValue(
      json({ name: 'de_overpass', pos_x: -4831, pos_y: 1781, scale: 5.2 }),
    )
    expect(await getMap('de_overpass')).toBeNull()
    expect(await getMap('de_overpass')).toMatchObject({ title: 'Overpass', scale: 5.2 })
  })
})

describe('uploadDemo', () => {
  // Succeeds every PUT at once.
  class FakeXHR {
    status = 200
    upload = { onprogress: null }
    onload: (() => void) | null = null
    open() {}
    send() {
      queueMicrotask(() => this.onload?.())
    }
  }

  it("opens the match the confirm returns, which may be an earlier upload's", async () => {
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
    fetchMock.mockImplementation(async (url: string) =>
      String(url).endsWith('/uploaded')
        ? json({ id: 'aaaaaaaaaaaa8', status: 'ready', duplicate: true })
        : json({ id: 'aaaaaaaaaaaa9', upload_url: 'https://r2.test/put', upload_expires_in: 3600 }, 201),
    )
    const cb = { onStage: () => {}, onProgress: () => {}, signal: new AbortController().signal }
    expect(await uploadDemo(new File(['PBDEMS2'], 'a.dem'), 'token', cb)).toBe('aaaaaaaaaaaa8')
    expect(urls()).toEqual(['/api/matches', '/api/matches/aaaaaaaaaaaa9/uploaded'])
  })
})

describe('getEvents', () => {
  it("reads the match's history, and has none for the sample", async () => {
    const row = { seq: 1, ts: 5, kind: 'status', msg: 'uploaded', attempt: null, build: null, parse_id: null }
    fetchMock.mockResolvedValue(json({ events: [row] }))
    expect(await getEvents('aaaaaaaaaaab4')).toEqual([row])
    expect(urls()).toEqual(['/api/matches/aaaaaaaaaaab4/events'])
    expect(await getEvents('sample')).toEqual([])
  })
})

describe('getPulse', () => {
  const ready = (id: string, parseId: string | null) =>
    fetchMock.mockImplementationOnce(async () => json(summary(id, 'ready', parseId)))

  it("reads the live parse's Pulse", async () => {
    const id = 'aaaaaaaaaab51'
    ready(id, PARSE)
    await getSummary(id)
    fetchMock.mockResolvedValueOnce(json({ pulse_version: 1, model: 'jev-1', rounds: [], worth_watching: [] }))
    expect(await getPulse(id)).toMatchObject({ model: 'jev-1' })
    expect(urls().at(-1)).toBe(`/api/matches/${id}/pulse.json?v=${PARSE}`)
  })

  it('is null on a 404 or an unknown version, and asks again next time', async () => {
    const id = 'aaaaaaaaaab52'
    ready(id, PARSE)
    await getSummary(id)
    fetchMock.mockResolvedValueOnce(json({ error: 'no pulse for this parse' }, 404))
    expect(await getPulse(id)).toBeNull()
    fetchMock.mockResolvedValueOnce(json({ pulse_version: 2 }))
    expect(await getPulse(id)).toBeNull()
    expect(urls().filter((u) => u.includes('pulse.json'))).toHaveLength(2)
  })

  it('is null without a parse id, without asking', async () => {
    const id = 'aaaaaaaaaab53'
    ready(id, null)
    await getSummary(id)
    expect(await getPulse(id)).toBeNull()
    expect(await getPulse('sample')).toBeNull()
    expect(urls().some((u) => u.includes('pulse.json'))).toBe(false)
  })
})

describe('the gallery', () => {
  const card = { id: 'aaaaaaaaaab61', map: 'de_inferno', score_a: 13, score_b: 11, parsed_at: 5 }

  it('lists the featured matches, and none from a Worker without the route', async () => {
    fetchMock.mockResolvedValueOnce(json([card]))
    expect(await getGallery()).toEqual([card])
    fetchMock.mockResolvedValueOnce(json({ error: 'not found' }, 404))
    expect(await getGallery()).toEqual([])
    expect(urls()).toEqual(['/api/gallery', '/api/gallery'])
  })

  it('features a match with the upload token', async () => {
    fetchMock.mockResolvedValue(json({ id: card.id, featured: true }))
    await featureMatch(card.id, 'token', true)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/matches/${card.id}/feature`)
    expect(init.method).toBe('POST')
    expect(init.headers).toMatchObject({ authorization: 'Bearer token' })
    expect(JSON.parse(String(init.body))).toEqual({ featured: true })
  })

  it('takes a radar from the bundle, else from the API', () => {
    expect(radarUrl('de_mirage')).toBe(MAPS.de_mirage.radar)
    expect(radarUrl('de_inferno')).toBe('/api/maps/de_inferno/radar.png')
    expect(radarUrl(null)).toBeNull()
  })

  it('shows only the sample in fixtures mode, with no backend', async () => {
    vi.stubEnv('VITE_FIXTURES', '1')
    vi.resetModules()
    const api = await import('./api')
    const winners = ['A', 'A', 'B']
    fetchMock.mockResolvedValue(
      json({
        schema_version: 1,
        parser_version: '0.1.0',
        match: { map: 'de_mirage', tick_rate: 64, demo_sha256: '' },
        players: [{ idx: 0, steam_id: '76561197960265728', name: 'someone', team: 'A' }],
        rounds: winners.map((winner, i) => ({ n: i + 1, winner, reason: 'elimination', side_a: 'CT' })),
        kills: [],
        scoreboard: [],
      }),
    )
    expect(await api.getGallery()).toEqual([{ id: 'sample', map: 'de_mirage', score_a: 2, score_b: 1, parsed_at: 0 }])
    await api.featureMatch('sample', '', true)
    expect(api.radarUrl('de_inferno')).toBeNull()
    expect(urls()).toEqual(['/samples/match.json'])
  })
})
