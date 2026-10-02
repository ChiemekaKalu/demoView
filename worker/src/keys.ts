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

export function roundKey(matchId: string, n: number): string {
  return `parsed/${matchId}/rounds/${String(n).padStart(2, "0")}.json`;
}
