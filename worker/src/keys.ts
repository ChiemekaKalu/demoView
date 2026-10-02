// Match ids and the R2 key layout from the spec.

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

export function matchFileKey(matchId: string): string {
  return `parsed/${matchId}/match.json`;
}

export function roundKey(matchId: string, n: number): string {
  return `parsed/${matchId}/rounds/${String(n).padStart(2, "0")}.json`;
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
