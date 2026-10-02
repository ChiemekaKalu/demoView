// Match and parse ids, and the R2 key layout from the spec.

// Crockford base32, lowercase. 32 symbols, so a random byte masked to 5 bits
// maps onto it without bias.
const ALPHABET = "0123456789abcdefghjkmnpqrstvwxyz";
const ID_LENGTH = 13; // 65 bits: unguessable enough for an unlisted share link

const ID_PATTERN = new RegExp(`^[${ALPHABET}]{${ID_LENGTH}}$`);

export function newMatchId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(ID_LENGTH));
  let id = "";
  for (const b of bytes) id += ALPHABET[b & 31];
  return id;
}

// Ids come from URLs and end up in R2 keys, so anything else is rejected.
export function isMatchId(s: string): boolean {
  return ID_PATTERN.test(s);
}

export function demoKey(matchId: string): string {
  return `demos/${matchId}.dem`;
}

// A parse id names the prefix one parse writes to: 9 characters of the time
// it started, then 7 random, so ids sort by age and cleanup can read a
// prefix's age from its name.
const PARSE_TIME_LENGTH = 9; // 45 bits of milliseconds: until the year 3084
const PARSE_ID_LENGTH = 16;
const PARSE_ID_PATTERN = new RegExp(`^[${ALPHABET}]{${PARSE_ID_LENGTH}}$`);

export function newParseId(now = Date.now()): string {
  let time = "";
  for (let t = now, i = 0; i < PARSE_TIME_LENGTH; i++, t = Math.floor(t / 32)) time = ALPHABET[t % 32] + time;
  const bytes = crypto.getRandomValues(new Uint8Array(PARSE_ID_LENGTH - PARSE_TIME_LENGTH));
  let id = time;
  for (const b of bytes) id += ALPHABET[b & 31];
  return id;
}

export function isParseId(s: string): boolean {
  return PARSE_ID_PATTERN.test(s);
}

// When the parse with this id started, in Unix ms.
export function parseIdTime(parseId: string): number {
  let t = 0;
  for (const c of parseId.slice(0, PARSE_TIME_LENGTH)) t = t * 32 + ALPHABET.indexOf(c);
  return t;
}

// A parse's files. A null parse id is the v1 layout, written straight under
// parsed/{match_id}/ before parses had their own prefix.
export function parsedPrefix(matchId: string, parseId: string | null): string {
  return parseId ? `parsed/${matchId}/${parseId}/` : `parsed/${matchId}/`;
}

export function matchFileKey(matchId: string, parseId: string | null): string {
  return `${parsedPrefix(matchId, parseId)}match.json`;
}

export function roundKey(matchId: string, parseId: string | null, n: number): string {
  return `${parsedPrefix(matchId, parseId)}rounds/${String(n).padStart(2, "0")}.json`;
}

// Map names come from URLs and end up in R2 keys, like match ids.
export function isMapName(s: string): boolean {
  return /^[a-z0-9_]{1,64}$/.test(s);
}

// The map files under maps/ come from MurkyYT/cs2-map-icons: an HLTV overview
// file and a radar image per map.
export function radarInfoKey(map: string): string {
  return `maps/radar_info/${map}.txt`;
}

export function radarImageKey(map: string): string {
  return `maps/radars/${map}_radar_psd.png`;
}
