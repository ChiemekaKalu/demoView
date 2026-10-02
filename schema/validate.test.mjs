// Checks that validate.mjs rejects pulse files that break its cross-field
// rules. Each case edits a copy of samples/pulse.json, writes it beside a copy
// of samples/match.json and runs the validator on it.
//
//   node --test validate.test.mjs     (part of npm test)

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SAMPLES = path.join(HERE, "samples");
const PULSE = JSON.parse(fs.readFileSync(path.join(SAMPLES, "pulse.json"), "utf8"));
const MATCH = JSON.parse(fs.readFileSync(path.join(SAMPLES, "match.json"), "utf8"));
const first = PULSE.rounds[0];
const firstInMatch = MATCH.rounds.find((rd) => rd.n === first.n);

function validate(edit, ...flags) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "demoview-pulse-"));
  try {
    fs.copyFileSync(path.join(SAMPLES, "match.json"), path.join(dir, "match.json"));
    const pulse = structuredClone(PULSE);
    edit(pulse);
    fs.writeFileSync(path.join(dir, "pulse.json"), JSON.stringify(pulse));
    const run = spawnSync(process.execPath, [path.join(HERE, "validate.mjs"), ...flags, path.join(dir, "pulse.json")], { encoding: "utf8" });
    return { status: run.status, output: run.stdout + run.stderr };
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

function rejects(name, edit, message) {
  test(name, () => {
    const { status, output } = validate(edit);
    assert.equal(status, 1, output);
    assert.match(output, message);
  });
}

test("the unedited sample passes --strict", () => {
  const { status, output } = validate(() => {}, "--strict");
  assert.equal(status, 0, output);
});

// (a) every round the pulse references exists in match.json
const missing = Math.max(...MATCH.rounds.map((rd) => rd.n)) + 1;
rejects("a round that match.json doesn't have", (p) => (p.rounds.at(-1).n = missing), new RegExp(`round ${missing} is not in match.json rounds`));
rejects("worth_watching naming a round the file doesn't have", (p) => (p.worth_watching[0] = missing), new RegExp(`round ${missing} is not in rounds`));

// (b) moment ticks strictly increase and fall inside the round
rejects("a moment on the same tick as the one before", (p) => (p.rounds[0].moments[2].tick = p.rounds[0].moments[1].tick), /moment ticks must strictly increase/);
rejects("a moment before the round starts", (p) => (p.rounds[0].moments[0].tick = firstInMatch.start_tick - 1), new RegExp(`tick ${firstInMatch.start_tick - 1} is outside round ${first.n}`));
rejects("a moment after the round ends", (p) => (p.rounds[0].moments.at(-1).tick = firstInMatch.end_tick + 1), new RegExp(`tick ${firstInMatch.end_tick + 1} is outside round ${first.n}`));

// (c) swing_tick is one of the round's moment ticks
rejects("a swing_tick between moments", (p) => (p.rounds[0].swing_tick = first.moments[0].tick + 1), /is not one of the round's moment ticks/);
rejects("a null swing_tick with several moments", (p) => (p.rounds[0].swing_tick = null), /swing_tick: null, but the round has/);
rejects("a swing_tick with a single moment", (p) => (p.rounds[0].moments = [first.moments[0]]), /must be null for a round with a single moment/);

test("a single moment with a null swing_tick passes", () => {
  const { status, output } = validate((p) => {
    p.rounds[0].moments = [first.moments[0]];
    p.rounds[0].swing_tick = null;
  });
  assert.equal(status, 0, output);
});

// the rest of the pulse rules
rejects("a duplicate round", (p) => p.rounds.push(structuredClone(first)), new RegExp(`duplicate round n ${first.n}`));
rejects("worth_watching out of highlight order", (p) => p.worth_watching.reverse(), /need highest highlight first/);
rejects(
  "how_won naming the losing side",
  (p) => (p.rounds[0].facts.how_won = first.facts.how_won.startsWith("ct_") ? "t_post_plant" : "ct_hold"),
  /how_won: .*, but the (CT|T) side won round/,
);
