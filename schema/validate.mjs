#!/usr/bin/env node
// Validates demoView parser output against schema v1.
//
//   node validate.mjs [--strict] <file | dir | glob>...
//
// The schema is picked by file name: match.json uses match.schema.json, NN.json
// (digits only) uses round.schema.json. A directory is searched recursively for
// both. A round file is also checked against its match.json: ../match.json next
// to it (added to the run automatically), or else the only match.json passed.
//
// Errors break a rule the spec states. Warnings flag a recommended convention
// the spec doesn't settle yet (see README.md); --strict makes them fatal.
// Exit code: 0 ok, 1 validation failed, 2 usage or I/O problem.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import Ajv2020 from "ajv/dist/2020.js";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const MAX_ERRORS_SHOWN = 50;
// A position change faster than this between consecutive alive frames is
// reported as a teleport. Running is ~250 u/s.
const TELEPORT_UNITS_PER_SEC = 1000;

// strictRequired is off because `then: { required: ["kill"] }` names a property
// declared in the parent schema, which is valid 2020-12.
const ajv = new Ajv2020({ allErrors: true, strict: true, strictRequired: false });
const schemaValidators = {
  match: ajv.compile(readSchema("match.schema.json")),
  round: ajv.compile(readSchema("round.schema.json")),
};

function readSchema(name) {
  return JSON.parse(fs.readFileSync(path.join(HERE, name), "utf8"));
}

// ---------------------------------------------------------------- arguments

function usage() {
  return [
    "usage: node validate.mjs [--strict] <file | dir | glob>...",
    "  match.json  -> match.schema.json",
    "  NN.json     -> round.schema.json (NN = round number)",
    "  dir         -> every match.json and NN.json under it",
    "  --strict    -> warnings fail the run too",
  ].join("\n");
}

function kindOf(file) {
  const base = path.basename(file);
  if (base === "match.json") return "match";
  if (/^\d+\.json$/.test(base)) return "round";
  return null;
}

function walk(dir, out) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(p, out);
    else if (entry.isFile() && kindOf(p)) out.push(p);
  }
}

// Shells on Windows don't expand globs, so handle * and ? in the last segment.
function expandGlob(arg) {
  const dir = path.dirname(arg);
  const re = new RegExp(
    "^" +
      path
        .basename(arg)
        .replace(/[.+^${}()|[\]\\]/g, "\\$&")
        .replace(/\*/g, ".*")
        .replace(/\?/g, ".") +
      "$",
  );
  if (!fs.existsSync(dir)) return [];
  return fs
    .readdirSync(dir)
    .filter((name) => re.test(name))
    .map((name) => path.join(dir, name));
}

function collectFiles(args) {
  const files = [];
  const problems = [];
  for (const arg of args) {
    if (/[*?]/.test(path.basename(arg))) {
      const matches = expandGlob(arg);
      if (matches.length === 0) problems.push(`no files match ${arg}`);
      files.push(...matches);
    } else if (!fs.existsSync(arg)) {
      problems.push(`not found: ${arg}`);
    } else if (fs.statSync(arg).isDirectory()) {
      const found = [];
      walk(arg, found);
      if (found.length === 0) problems.push(`no match.json or NN.json files under ${arg}`);
      files.push(...found);
    } else {
      files.push(arg);
    }
  }
  return { files: [...new Set(files.map((f) => path.resolve(f)))], problems };
}

// ---------------------------------------------------------------- reporting

class Report {
  constructor(file, kind) {
    this.file = file;
    this.kind = kind;
    this.errors = [];
    this.warnings = [];
    this.notes = [];
    this.data = undefined;
    this.schemaOk = false;
  }
  error(msg) {
    this.errors.push(msg);
  }
  warn(msg) {
    this.warnings.push(msg);
  }
  note(msg) {
    this.notes.push(msg);
  }
}

function display(file) {
  const rel = path.relative(process.cwd(), file);
  return rel && !rel.startsWith("..") ? rel : file;
}

function formatAjvError(e) {
  const at = e.instancePath || "(root)";
  switch (e.keyword) {
    case "additionalProperties":
      return `${at}: unknown property "${e.params.additionalProperty}"`;
    case "required":
      return `${at}: missing required property "${e.params.missingProperty}"`;
    case "enum":
      return `${at}: must be one of ${e.params.allowedValues.map((v) => JSON.stringify(v)).join(", ")}`;
    case "const":
      return `${at}: must be ${JSON.stringify(e.params.allowedValue)}`;
    case "not":
      // only used by events: else { not: { required: ["kill"] } }
      return e.schemaPath.endsWith("/else/not") ? `${at}: only events of type "kill" may have "kill"` : `${at}: ${e.message}`;
    default:
      return `${at}: ${e.message}`;
  }
}

// ---------------------------------------------------------------- loading

function load(report) {
  let text;
  try {
    text = fs.readFileSync(report.file, "utf8");
  } catch (err) {
    report.error(`cannot read file: ${err.message}`);
    return;
  }
  try {
    report.data = JSON.parse(text);
  } catch (err) {
    report.error(`invalid JSON: ${err.message}`);
    return;
  }
  const validate = schemaValidators[report.kind];
  if (validate(report.data)) {
    report.schemaOk = true;
  } else {
    const seen = new Set();
    for (const e of validate.errors) {
      if (e.keyword === "if") continue; // the failing then/else branch is reported on its own
      const msg = formatAjvError(e);
      if (!seen.has(msg)) {
        seen.add(msg);
        report.error(msg);
      }
    }
    report.note("cross-field checks skipped until the schema errors are fixed");
  }
}

// ---------------------------------------------------------------- match.json

function indexBy(list, key) {
  const map = new Map();
  for (const item of list) map.set(item[key], item);
  return map;
}

function checkMatch(r) {
  const m = r.data;

  // players: unique idx
  const seenIdx = new Set();
  m.players.forEach((p, i) => {
    if (seenIdx.has(p.idx)) r.error(`/players/${i}/idx: duplicate idx ${p.idx}`);
    seenIdx.add(p.idx);
    if (p.idx !== i) r.warn(`/players/${i}/idx: idx ${p.idx} != array position ${i} (recommended: idx = position)`);
  });
  const players = indexBy(m.players, "idx");
  const playerRef = (where, idx) => {
    if (!players.has(idx)) r.error(`${where}: player idx ${idx} is not in players`);
  };

  // rounds: unique n, tick order
  const seenN = new Set();
  m.rounds.forEach((rd, i) => {
    const at = `/rounds/${i}`;
    if (seenN.has(rd.n)) r.error(`${at}/n: duplicate round n ${rd.n}`);
    seenN.add(rd.n);
    if (!(rd.start_tick <= rd.freeze_end_tick && rd.freeze_end_tick <= rd.end_tick)) {
      r.error(
        `${at}: ticks out of order: need start_tick <= freeze_end_tick <= end_tick, got ${rd.start_tick}, ${rd.freeze_end_tick}, ${rd.end_tick}`,
      );
    }
    if (rd.n !== i + 1) r.warn(`${at}/n: n ${rd.n} at position ${i} (recommended: rounds numbered 1..N in order)`);
    const next = m.rounds[i + 1];
    if (next && next.start_tick < rd.end_tick) {
      r.warn(`${at}: round ${rd.n} ends at tick ${rd.end_tick}, after round ${next.n} starts at ${next.start_tick}`);
    }
  });
  if (m.rounds.length > 0 && m.rounds[0].side_a !== "CT") {
    r.warn(`/rounds/0/side_a: team A plays T in the first round (recommended: A is the team that starts on CT)`);
  }
  const rounds = indexBy(m.rounds, "n");
  const byStart = [...m.rounds].sort((a, b) => a.start_tick - b.start_tick);

  // kills: references, ordering, tick inside its round
  m.kills.forEach((k, i) => {
    const at = `/kills/${i}`;
    if (!rounds.has(k.round)) r.error(`${at}/round: round ${k.round} is not in rounds`);
    if (k.attacker !== null) playerRef(`${at}/attacker`, k.attacker);
    playerRef(`${at}/victim`, k.victim);
    if (k.assister !== null) {
      playerRef(`${at}/assister`, k.assister);
      if (k.assister === k.attacker || k.assister === k.victim) {
        r.warn(`${at}/assister: assister ${k.assister} is also the attacker or the victim`);
      }
    }
    if (i > 0 && k.tick < m.kills[i - 1].tick) {
      r.warn(`${at}/tick: kills not sorted by tick (${k.tick} after ${m.kills[i - 1].tick})`);
    }
    const rd = rounds.get(k.round);
    if (rd) {
      const next = byStart[byStart.indexOf(rd) + 1];
      if (k.tick < rd.start_tick || (next && k.tick >= next.start_tick)) {
        r.warn(
          `${at}/tick: tick ${k.tick} is outside round ${k.round} (starts ${rd.start_tick}` +
            (next ? `, next round starts ${next.start_tick})` : ")"),
        );
      }
    }
  });

  // scoreboard: references, one row per player, consistent with the kill list
  const seenRow = new Set();
  m.scoreboard.forEach((row, i) => {
    const at = `/scoreboard/${i}`;
    playerRef(`${at}/player`, row.player);
    if (seenRow.has(row.player)) r.error(`${at}/player: duplicate scoreboard row for player ${row.player}`);
    seenRow.add(row.player);
  });
  for (const p of m.players) {
    if (!seenRow.has(p.idx)) r.warn(`/scoreboard: no row for player ${p.idx} (recommended: one row per player)`);
  }
  checkScoreboardAgainstKills(r, m, players);
}

// Recounts the scoreboard from the kill list. Only a warning: whether team
// kills and suicides subtract (as in game) and whether hs_pct is 0-1 or 0-100
// are open questions. Kills may match either the enemy-kill count or the
// in-game count; hs_pct is checked on the recommended 0-100 scale.
function checkScoreboardAgainstKills(r, m, players) {
  const tally = new Map(m.players.map((p) => [p.idx, { enemy: 0, hs: 0, penalty: 0, deaths: 0, assists: 0 }]));
  for (const k of m.kills) {
    if (tally.has(k.victim)) tally.get(k.victim).deaths++;
    if (k.assister !== null && tally.has(k.assister)) tally.get(k.assister).assists++;
    if (k.attacker === null || !tally.has(k.attacker)) continue;
    const t = tally.get(k.attacker);
    const victim = players.get(k.victim);
    const sameTeam = victim && victim.team === players.get(k.attacker).team;
    if (k.attacker === k.victim || sameTeam) {
      t.penalty++;
    } else {
      t.enemy++;
      if (k.headshot) t.hs++;
    }
  }
  m.scoreboard.forEach((row, i) => {
    const t = tally.get(row.player);
    if (!t) return;
    const at = `/scoreboard/${i}`;
    if (row.kills !== t.enemy && row.kills !== t.enemy - t.penalty) {
      r.warn(`${at}/kills: ${row.kills}, but the kill list has ${t.enemy} enemy kills (${t.penalty} team kills/suicides)`);
    }
    if (row.deaths !== t.deaths) r.warn(`${at}/deaths: ${row.deaths}, but the kill list has ${t.deaths}`);
    if (row.assists !== t.assists) r.warn(`${at}/assists: ${row.assists}, but the kill list has ${t.assists}`);
    const hsPct = t.enemy === 0 ? 0 : (100 * t.hs) / t.enemy;
    if (Math.abs(row.hs_pct - hsPct) > 0.051) {
      r.warn(`${at}/hs_pct: ${row.hs_pct}, but the kill list gives ${hsPct.toFixed(1)} (${t.hs}/${t.enemy}, 0-100 scale)`);
    }
  });
}

// ---------------------------------------------------------------- rounds/NN.json

function checkRound(r, matchReport) {
  const rf = r.data;
  const F = rf.frames;
  const inRange = (f) => f >= 0 && f < F;

  // file name NN must be the round number
  const nn = path.basename(r.file, ".json");
  if (Number(nn) !== rf.round) r.error(`file name ${nn}.json does not match round ${rf.round}`);
  else if (nn !== String(rf.round).padStart(2, "0")) {
    r.warn(`file name ${nn}.json is not zero-padded to two digits (recommended: ${String(rf.round).padStart(2, "0")}.json)`);
  }

  // players: unique idx, column lengths, life and movement
  const seenIdx = new Set();
  const maxStep = TELEPORT_UNITS_PER_SEC / rf.hz;
  rf.players.forEach((p, i) => {
    const at = `/players/${i}`;
    if (seenIdx.has(p.idx)) r.error(`${at}/idx: duplicate idx ${p.idx}`);
    seenIdx.add(p.idx);
    let lengthsOk = true;
    for (const col of ["x", "y", "yaw", "hp"]) {
      if (p[col].length !== F) {
        r.error(`${at}/${col}: ${p[col].length} values, expected frames = ${F} (player idx ${p.idx})`);
        lengthsOk = false;
      }
    }
    if (!lengthsOk) return;
    let deadAt = p.hp[0] === 0 ? 0 : -1;
    let teleports = 0;
    for (let f = 1; f < F; f++) {
      if (deadAt < 0 && p.hp[f] === 0) deadAt = f;
      if (deadAt >= 0 && f > deadAt) {
        if (p.hp[f] > 0) {
          r.warn(`${at}/hp/${f}: player idx ${p.idx} is alive again after dying at frame ${deadAt}`);
          break;
        }
        if (p.x[f] !== p.x[f - 1] || p.y[f] !== p.y[f - 1]) {
          r.warn(`${at}: player idx ${p.idx} moves at frame ${f} after dying at frame ${deadAt} (recommended: hold the death position)`);
          break;
        }
      }
      if (p.hp[f] > 0 && p.hp[f - 1] > 0) {
        const step = Math.hypot(p.x[f] - p.x[f - 1], p.y[f] - p.y[f - 1]);
        if (step > maxStep && teleports++ === 0) {
          r.warn(`${at}: player idx ${p.idx} jumps ${Math.round(step)} units between frames ${f - 1} and ${f} (over ${TELEPORT_UNITS_PER_SEC} u/s)`);
        }
      }
    }
  });
  const tracks = indexBy(rf.players, "idx");

  // grenades: frame ranges and order, path sampling
  rf.grenades.forEach((g, i) => {
    const at = `/grenades/${i}`;
    for (const key of ["throw_frame", "detonate_frame", "end_frame"]) {
      if (!inRange(g[key])) r.error(`${at}/${key}: frame ${g[key]} is outside [0, ${F})`);
    }
    if (!(g.throw_frame <= g.detonate_frame && g.detonate_frame <= g.end_frame)) {
      r.error(
        `${at}: frames out of order: need throw_frame <= detonate_frame <= end_frame, got ${g.throw_frame}, ${g.detonate_frame}, ${g.end_frame}`,
      );
    } else if (g.path.length !== g.detonate_frame - g.throw_frame + 1) {
      r.warn(
        `${at}/path: ${g.path.length} points (recommended: one per frame from throw_frame to detonate_frame = ${g.detonate_frame - g.throw_frame + 1})`,
      );
    }
    if (!tracks.has(g.thrower)) r.warn(`${at}/thrower: player idx ${g.thrower} has no track in this round`);
  });

  // events: frame range and order
  rf.events.forEach((e, i) => {
    if (!inRange(e.frame)) r.error(`/events/${i}/frame: frame ${e.frame} is outside [0, ${F})`);
    if (i > 0 && e.frame < rf.events[i - 1].frame) r.warn(`/events/${i}/frame: events not sorted by frame`);
  });

  // cross-file checks against match.json
  if (!matchReport) {
    r.note("no match.json next to it or passed alongside; references into match.json not checked");
    return;
  }
  if (!matchReport.schemaOk) {
    r.note(`${display(matchReport.file)} is invalid; references into it not checked`);
    return;
  }
  r.note(`checked against ${display(matchReport.file)}`);
  const m = matchReport.data;
  const players = indexBy(m.players, "idx");
  const matchRound = m.rounds.find((rd) => rd.n === rf.round);
  if (!matchRound) r.error(`/round: round ${rf.round} is not in match.json rounds`);
  else if (rf.start_tick !== matchRound.freeze_end_tick) {
    r.warn(`/start_tick: ${rf.start_tick} != match.json freeze_end_tick ${matchRound.freeze_end_tick} (recommended: frame 0 = freeze end)`);
  }
  rf.players.forEach((p, i) => {
    if (!players.has(p.idx)) r.error(`/players/${i}/idx: player idx ${p.idx} is not in match.json players`);
  });
  rf.grenades.forEach((g, i) => {
    if (!players.has(g.thrower)) r.error(`/grenades/${i}/thrower: player idx ${g.thrower} is not in match.json players`);
  });

  const ticksPerFrame = m.match.tick_rate / rf.hz;
  const killsWithEvent = new Set();
  rf.events.forEach((e, i) => {
    if (e.type !== "kill") return;
    const at = `/events/${i}/kill`;
    if (e.kill >= m.kills.length) {
      r.error(`${at}: ${e.kill} is out of range; match.json has ${m.kills.length} kills`);
      return;
    }
    const k = m.kills[e.kill];
    killsWithEvent.add(e.kill);
    if (k.round !== rf.round) {
      r.error(`${at}: kill ${e.kill} belongs to round ${k.round}, not round ${rf.round}`);
      return;
    }
    const expected = Math.ceil((k.tick - rf.start_tick) / ticksPerFrame);
    if (e.frame !== expected) {
      r.warn(`/events/${i}/frame: ${e.frame}, but kill ${e.kill} at tick ${k.tick} maps to frame ${expected} (recommended: ceil((tick - start_tick) * hz / tick_rate))`);
    }
    const v = tracks.get(k.victim);
    if (!v) {
      r.warn(`${at}: victim idx ${k.victim} has no track in this round`);
    } else if (inRange(e.frame) && v.hp.length === F && (v.hp[e.frame] !== 0 || (e.frame > 0 && v.hp[e.frame - 1] === 0))) {
      r.warn(`/events/${i}: victim idx ${k.victim} should first have hp 0 at frame ${e.frame}`);
    }
  });
  m.kills.forEach((k, i) => {
    if (k.round === rf.round && !killsWithEvent.has(i)) r.warn(`/events: no kill event for match.json kill ${i} (round ${rf.round})`);
  });
}

// ---------------------------------------------------------------- main

function main(argv) {
  const strict = argv.includes("--strict");
  const args = argv.filter((a) => a !== "--strict");
  if (args.length === 0 || args.includes("--help") || args.includes("-h")) {
    console.error(usage());
    return 2;
  }
  const unknownFlag = args.find((a) => a.startsWith("--"));
  if (unknownFlag) {
    console.error(`unknown option ${unknownFlag}\n${usage()}`);
    return 2;
  }

  const { files, problems } = collectFiles(args);
  for (const p of problems) console.error(`error: ${p}`);
  if (problems.length > 0) return 2;

  const reports = new Map();
  const add = (file) => {
    const kind = kindOf(file);
    if (!kind) {
      console.error(`error: ${display(file)}: can't tell which schema to use; expected match.json or NN.json`);
      return false;
    }
    if (!reports.has(file)) reports.set(file, new Report(file, kind));
    return true;
  };
  if (!files.every(add)) return 2;

  // Pull in ../match.json for round files that have one. Loop over a copy,
  // since this adds to `reports`.
  for (const rep of Array.from(reports.values())) {
    if (rep.kind !== "round") continue;
    const sibling = path.resolve(path.dirname(rep.file), "..", "match.json");
    if (fs.existsSync(sibling) && !reports.has(sibling)) {
      add(sibling);
      reports.get(sibling).note("added: found next to a round file");
    }
  }

  const all = [...reports.values()].sort(
    (a, b) => (a.kind === b.kind ? a.file.localeCompare(b.file) : a.kind === "match" ? -1 : 1),
  );
  for (const rep of all) load(rep);

  const matchReports = all.filter((rep) => rep.kind === "match");
  for (const rep of all) {
    if (!rep.schemaOk) continue;
    if (rep.kind === "match") {
      checkMatch(rep);
    } else {
      const sibling = reports.get(path.resolve(path.dirname(rep.file), "..", "match.json"));
      checkRound(rep, sibling ?? (matchReports.length === 1 ? matchReports[0] : undefined));
    }
  }

  let failed = 0;
  let errorCount = 0;
  let warningCount = 0;
  for (const rep of all) {
    const fails = rep.errors.length > 0 || (strict && rep.warnings.length > 0);
    if (fails) failed++;
    errorCount += rep.errors.length;
    warningCount += rep.warnings.length;
    const extra = rep.notes.length ? `  (${rep.notes.join("; ")})` : "";
    console.log(`${fails ? "FAIL" : "ok  "} ${display(rep.file)}${extra}`);
    const lines = [...rep.errors.map((e) => `  error: ${e}`), ...rep.warnings.map((w) => `  warning: ${w}`)];
    for (const line of lines.slice(0, MAX_ERRORS_SHOWN)) console.log(line);
    if (lines.length > MAX_ERRORS_SHOWN) console.log(`  ... and ${lines.length - MAX_ERRORS_SHOWN} more`);
  }
  console.log(
    `\n${all.length} file(s): ${all.length - failed} passed, ${failed} failed; ${errorCount} error(s), ${warningCount} warning(s)` +
      (strict ? " [--strict: warnings fail]" : ""),
  );
  return failed > 0 ? 1 : 0;
}

process.exitCode = main(process.argv.slice(2));
