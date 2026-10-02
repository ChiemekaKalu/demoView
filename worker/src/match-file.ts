// The parts of match.json (see schema/match.schema.json) the Worker copies
// into D1.

export const SCHEMA_VERSION = 1;

export type Team = "A" | "B";

export interface MatchFile {
  schema_version: number;
  parser_version: string;
  match: { map: string; tick_rate: number; demo_sha256: string };
  players: { idx: number; steam_id: string; name: string; team: Team }[];
  rounds: { n: number; winner: Team; reason: string; side_a: "CT" | "T" }[];
  kills: {
    round: number;
    tick: number;
    attacker: number | null;
    victim: number;
    weapon: string;
    headshot: boolean;
  }[];
  scoreboard: {
    player: number;
    kills: number;
    deaths: number;
    assists: number;
    adr: number;
    hs_pct: number;
  }[];
}

// A shape check, not full validation: CI validates parser output against the
// JSON Schema. This only stops a wrong or mismatched body from reaching D1.
export function asMatchFile(body: unknown): MatchFile {
  if (!isObject(body)) throw new Error("match.json is not an object");
  if (body.schema_version !== SCHEMA_VERSION) {
    throw new Error(
      `match.json has schema_version ${String(body.schema_version)}, expected ${SCHEMA_VERSION}`,
    );
  }
  if (typeof body.parser_version !== "string") throw new Error("match.json has no parser_version");
  if (!isObject(body.match) || typeof body.match.map !== "string") {
    throw new Error("match.json has no match.map");
  }
  for (const key of ["players", "rounds", "kills", "scoreboard"]) {
    if (!Array.isArray(body[key])) throw new Error(`match.json ${key} is not an array`);
  }
  return body as unknown as MatchFile;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
