import type { Side } from './types'

const REASONS: Record<string, string> = {
  elimination: 'Elimination',
  bomb_exploded: 'Bomb exploded',
  bomb_defused: 'Bomb defused',
  time_expired: 'Time ran out',
  surrender: 'Surrender',
}

const SHORT_REASONS: Record<string, string> = {
  bomb_exploded: 'Exploded',
  bomb_defused: 'Defused',
  time_expired: 'Time',
}

// For tight spaces such as the round tiles.
export function reasonShort(reason: string | null): string {
  return (reason && SHORT_REASONS[reason]) || reasonLabel(reason)
}

export function reasonLabel(reason: string | null): string {
  if (!reason) return 'Unknown'
  return REASONS[reason] ?? reason.replace(/_/g, ' ')
}

const WEAPONS: Record<string, string> = {
  ak47: 'AK-47',
  m4a1: 'M4A4',
  m4a1_silencer: 'M4A1-S',
  awp: 'AWP',
  ssg08: 'SSG 08',
  usp_silencer: 'USP-S',
  hkp2000: 'P2000',
  glock: 'Glock-18',
  p250: 'P250',
  deagle: 'Desert Eagle',
  fiveseven: 'Five-SeveN',
  tec9: 'Tec-9',
  cz75a: 'CZ75-Auto',
  revolver: 'R8 Revolver',
  elite: 'Dual Berettas',
  galilar: 'Galil AR',
  famas: 'FAMAS',
  aug: 'AUG',
  sg556: 'SG 553',
  mp9: 'MP9',
  mac10: 'MAC-10',
  mp7: 'MP7',
  mp5sd: 'MP5-SD',
  ump45: 'UMP-45',
  p90: 'P90',
  bizon: 'PP-Bizon',
  nova: 'Nova',
  xm1014: 'XM1014',
  mag7: 'MAG-7',
  sawedoff: 'Sawed-Off',
  negev: 'Negev',
  m249: 'M249',
  g3sg1: 'G3SG1',
  scar20: 'SCAR-20',
  hegrenade: 'HE grenade',
  inferno: 'Fire',
  knife: 'Knife',
  taser: 'Zeus',
  world: 'World',
}

export function weaponLabel(weapon: string | null): string {
  if (!weapon) return ''
  return WEAPONS[weapon] ?? (weapon.startsWith('knife') ? 'Knife' : weapon.replace(/_/g, ' '))
}

// The side a team played in a round, from the round's side_a.
export function sideOf(team: string | null | undefined, sideA: string | null | undefined): Side {
  const a: Side = sideA === 'T' ? 'T' : 'CT'
  return team === 'B' ? (a === 'CT' ? 'T' : 'CT') : a
}

// A shape for each side, so sides differ by more than color. Matches the
// canvas: CT is a circle, T a square.
export const SIDE_GLYPH: Record<Side, string> = { CT: '●', T: '■' }

const GALLERY_DATE = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' })

// A gallery card's date, in the viewer's locale: "Oct 2, 2026".
export function galleryDate(ms: number): string {
  return GALLERY_DATE.format(ms)
}
