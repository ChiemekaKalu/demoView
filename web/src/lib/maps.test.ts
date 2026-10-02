import { describe, expect, it } from 'vitest'
import { fitMap, MAPS, mapTitle, radarToWorld, worldToRadar } from './maps'
import type { RoundFile } from './types'

const mirage = MAPS.de_mirage

describe('worldToRadar', () => {
  it('uses the verified Mirage constants', () => {
    expect(mirage).toMatchObject({ posX: -3230, posY: 1713, scale: 5, size: 1024 })
  })

  it('maps the radar origin to pixel 0,0', () => {
    expect(worldToRadar(mirage, -3230, 1713)).toEqual([0, 0])
  })

  it('maps the far corner to the image size', () => {
    expect(worldToRadar(mirage, -3230 + 5120, 1713 - 5120)).toEqual([1024, 1024])
  })

  it('flips y: higher game y is higher up the image', () => {
    const [, low] = worldToRadar(mirage, 0, -1000)
    const [, high] = worldToRadar(mirage, 0, 1000)
    expect(high).toBeLessThan(low)
  })

  it('follows px = (x - pos_x) / scale, py = (pos_y - y) / scale', () => {
    expect(worldToRadar(mirage, -1230, -287)).toEqual([400, 400])
  })

  it('round-trips through radarToWorld', () => {
    const [px, py] = worldToRadar(mirage, 123, -456)
    expect(radarToWorld(mirage, px, py)).toEqual([123, -456])
  })
})

describe('fitMap', () => {
  const round = {
    players: [{ idx: 0, x: [-100, 300], y: [0, 200], yaw: [0, 0], hp: [100, 100] }],
    grenades: [{ type: 'smoke', thrower: 0, throw_frame: 0, detonate_frame: 1, end_frame: 1, path: [[0, -200], [0, -200]] }],
  } as unknown as RoundFile

  it('puts every point inside the image', () => {
    const map = fitMap(round)
    for (const [x, y] of [[-100, 0], [300, 200], [0, -200]]) {
      const [px, py] = worldToRadar(map, x, y)
      expect(px).toBeGreaterThan(0)
      expect(px).toBeLessThan(map.size)
      expect(py).toBeGreaterThan(0)
      expect(py).toBeLessThan(map.size)
    }
    expect(map.fitted).toBe(true)
    expect(map.radar).toBeNull()
  })
})

describe('mapTitle', () => {
  it('names known and unknown maps', () => {
    expect(mapTitle('de_mirage')).toBe('Mirage')
    expect(mapTitle('de_ancient')).toBe('Ancient')
    expect(mapTitle(null)).toBe('Unknown map')
  })
})
