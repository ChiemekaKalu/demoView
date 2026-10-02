// Writes the Pulse eval's golden states (test/pulse-golden/) from the two
// public fixtures' parsed output, with the real state builder. Run it after
// any change to the states or questions, and review the diff:
//
//   npm run pulse:golden -- <reference dir>
//
// where <reference dir> holds <fixture>/match.json and rounds/NN.json, such as
// parser/testdata/reference. Players are p<idx> in every state, and the copy of
// match.json here has names and Steam ids blanked.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { roundFacts } from "../src/pulse/model";
import { type PulseMatch, RoundView, STATE_VERSION } from "../src/pulse/state";

const FIXTURES = ["valve_mm_mirage_2024", "faceit_nuke_2026"];
const OUT = join("test", "pulse-golden"); // npm scripts run in worker/

const reference = process.argv[2];
if (!reference) throw new Error("usage: pulse-golden <reference dir>");
mkdirSync(OUT, { recursive: true });

for (const fixture of FIXTURES) {
  const dir = join(reference, fixture);
  const match = JSON.parse(readFileSync(join(dir, "match.json"), "utf8"));
  const moments = [];
  const rounds = [];
  for (const r of (match as PulseMatch).rounds) {
    if (r.reason === "surrender") continue;
    const path = join(dir, "rounds", `${String(r.n).padStart(2, "0")}.json`);
    const view = new RoundView(match, r.n, existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : null);
    const ctWon = view.winner === "CT";
    for (const cut of view.cuts) {
      const e = view.events[cut]!;
      moments.push({ round: r.n, tick: e.tick, sec: e.sec, alive: e.alive, ct_won: ctWon, state: view.state({ cut }) });
    }
    rounds.push({ n: r.n, facts: roundFacts(view), state: view.state({ outcome: true, ids: true }) });
  }
  // The eval assembles a pulse.json and validates it against this.
  const blanked = {
    ...match,
    players: match.players.map((p: { idx: number }) => ({ ...p, name: `p${p.idx}`, steam_id: "0" })),
  };
  const golden = { fixture, state_version: STATE_VERSION, match: blanked, moments, rounds };
  writeFileSync(join(OUT, `${fixture}.json`), `${JSON.stringify(golden, null, 1)}\n`);
  console.log(`${fixture}: ${moments.length} moments, ${rounds.length} rounds`);
}
