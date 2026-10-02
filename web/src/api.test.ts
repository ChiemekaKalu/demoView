import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getMatchFile, getRound, getSummary } from './api'

// The module caches per match id, so each test uses its own id.
let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

const summary = (id: string, status: string, parsedAt: number | null = null) => ({
  id,
  status,
  error: null,
  map: 'de_mirage',
  score_a: 13,
  score_b: 9,
  parser_version: '0.1.0',
  schema_version: 1,
  created_at: 1,
  parsed_at: parsedAt,
  players: [],
  rounds: [{ n: 1, winner: 'A', reason: 'elimination', side_a: 'CT' }],
})

const urls = () => fetchMock.mock.calls.map(([url]) => String(url))

describe('getSummary', () => {
  it('keeps a ready summary for the session unless asked for a fresh one', async () => {
    const id = 'aaaaaaaaaaaa1'
    fetchMock.mockImplementation(async () => json(summary(id, 'ready', 5)))
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
  it('carry the ready summary\'s parsed_at, so a reparse is never served stale', async () => {
    const id = 'aaaaaaaaaaaa4'
    fetchMock.mockImplementation(async (url: string) =>
      String(url).endsWith(`/api/matches/${id}`) ? json(summary(id, 'ready', 1790935516715)) : json({}),
    )
    await getSummary(id)
    await getRound(id, 1)
    await getMatchFile(id)
    expect(urls()).toContain(`/api/matches/${id}/rounds/1?v=1790935516715`)
    expect(urls()).toContain(`/api/matches/${id}/match.json?v=1790935516715`)
  })
})

describe('getRound and getMatchFile', () => {
  it('give null on a 404 instead of failing', async () => {
    fetchMock.mockResolvedValue(json({ error: 'round not found' }, 404))
    expect(await getRound('aaaaaaaaaaaa5', 3)).toBeNull()
    expect(await getMatchFile('aaaaaaaaaaaa5')).toBeNull()
  })

  it('fetch each round once', async () => {
    fetchMock.mockImplementation(async () => json({ round: 2 }))
    await getRound('aaaaaaaaaaaa6', 2)
    await getRound('aaaaaaaaaaaa6', 2)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('retry a round whose fetch failed', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('offline')).mockImplementation(async () => json({ round: 2 }))
    await expect(getRound('aaaaaaaaaaaa7', 2)).rejects.toThrow('Could not reach the server.')
    expect(await getRound('aaaaaaaaaaaa7', 2)).toEqual({ round: 2 })
  })
})
