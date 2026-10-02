// Per-map radar constants. A radar image is `size` x `size` pixels; game
// coordinates map onto it with
//   px = (x - posX) / scale,  py = (posY - y) / scale
// (game y points up, image y points down).

import type { RoundFile } from './types'

export interface MapInfo {
  title: string
  posX: number
  posY: number
  scale: number
  size: number
  // The radar image's URL. The image is optional; a placeholder is drawn when
  // it is missing.
  radar: string | null
  // True when the constants were fitted to the data instead of known.
  fitted?: boolean
}

// Maps bundled with the viewer, so fixtures mode has a radar with no backend.
// Every other map comes from the API (apiMap). The image in public/radar/ is
// from MurkyYT/cs2-map-icons (images/radars/<map>_radar_psd.png, commit
// ae7ed6a), which extracts it from the CS2 depot, and the constants match that
// repo's data/radar_info/<map>.txt.
export const MAPS: Record<string, MapInfo> = {
  de_mirage: {
    title: 'Mirage',
    posX: -3230,
    posY: 1713,
    scale: 5,
    size: 1024,
    radar: `${import.meta.env.BASE_URL}radar/de_mirage.png`,
  },
}

// GET /api/maps/:name, whose numbers fit a radar 1024 pixels square.
export interface RadarInfo {
  name: string
  pos_x: number
  pos_y: number
  scale: number
}

export function apiMap(info: RadarInfo, radar: string): MapInfo {
  return { title: mapTitle(info.name), posX: info.pos_x, posY: info.pos_y, scale: info.scale, size: 1024, radar }
}

export function mapTitle(name: string | null | undefined): string {
  if (!name) return 'Unknown map'
  const known = MAPS[name]
  if (known) return known.title
  const bare = name.replace(/^[a-z]+_/, '')
  return bare.charAt(0).toUpperCase() + bare.slice(1)
}

export function worldToRadar(map: MapInfo, x: number, y: number): [number, number] {
  return [(x - map.posX) / map.scale, (map.posY - y) / map.scale]
}

export function radarToWorld(map: MapInfo, px: number, py: number): [number, number] {
  return [px * map.scale + map.posX, map.posY - py * map.scale]
}

// For a map with no known constants: a square that fits every position and
// grenade point in the round, with a margin, so the replay still draws.
export function fitMap(round: RoundFile, size = 1024, margin = 0.06): MapInfo {
  let minX = Infinity
  let maxX = -Infinity
  let minY = Infinity
  let maxY = -Infinity
  const add = (x: number, y: number) => {
    if (x < minX) minX = x
    if (x > maxX) maxX = x
    if (y < minY) minY = y
    if (y > maxY) maxY = y
  }
  for (const p of round.players) for (let i = 0; i < p.x.length; i++) add(p.x[i], p.y[i])
  for (const g of round.grenades) for (const [x, y] of g.path) add(x, y)
  if (!Number.isFinite(minX)) {
    minX = minY = -1000
    maxX = maxY = 1000
  }
  const span = Math.max(maxX - minX, maxY - minY, 1) * (1 + 2 * margin)
  const cx = (minX + maxX) / 2
  const cy = (minY + maxY) / 2
  return { title: 'Fitted', posX: cx - span / 2, posY: cy + span / 2, scale: span / size, size, radar: null, fitted: true }
}

