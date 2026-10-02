#!/usr/bin/env node
// Generates the synthetic sample data in this folder: match.json and a few
// rounds/NN.json. Everything is derived from a seeded PRNG, so running it again
// produces identical files.
//
//   node samples/generate.mjs        (or: npm run generate)
//
// The data is SYNTHETIC: fake players and Steam ids, scripted round outcomes,
// and movement along a hand-made waypoint graph whose coordinates only
// approximate de_mirage callouts. It follows schema v1 plus the recommended
// conventions in ../README.md (frame 0 = freeze end, kill frame = first frame
// the victim has hp 0, path = one point per frame from throw to detonation,
// yaw = integer degrees in [0, 360) counterclockwise from +x, hs_pct 0-100).

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const OUT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SEED = 20261002;

const TICK_RATE = 64;
const HZ = 32;
const TICKS_PER_FRAME = TICK_RATE / HZ;
const FREEZE_S = 15;
const POST_ROUND_S = 7;
const HALFTIME_S = 15;
const ROUND_TIME_S = 115;
const BOMB_TIMER_S = 40;
const DUELS_PER_S = 0.72; // chance per second that players in range start a duel
const SIM_FRAMES = 132 * HZ; // longer than any round
const FIRST_ROUND_START_TICK = 11563; // after warmup
const ROUND_FILES = [1, 2, 16, 21];

// de_mirage radar: pos_x -3230, pos_y 1713, scale 5.0 on a 1024 px image.
const RADAR = { minX: -3230, maxX: -3230 + 1024 * 5, minY: 1713 - 1024 * 5, maxY: 1713 };

// ------------------------------------------------------------------ PRNG

function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

class Rng {
  constructor(seed) {
    this.next = mulberry32(seed);
  }
  float(lo = 0, hi = 1) {
    return lo + (hi - lo) * this.next();
  }
  int(lo, hi) {
    return lo + Math.floor(this.next() * (hi - lo + 1));
  }
  chance(p) {
    return this.next() < p;
  }
  pick(list) {
    return list[Math.floor(this.next() * list.length)];
  }
  weighted(weights) {
    // weights: { value: weight }
    const entries = Object.entries(weights);
    let r = this.next() * entries.reduce((s, [, w]) => s + w, 0);
    for (const [value, w] of entries) {
      r -= w;
      if (r < 0) return Number(value);
    }
    return Number(entries[entries.length - 1][0]);
  }
  shuffle(list) {
    const out = [...list];
    for (let i = out.length - 1; i > 0; i--) {
      const j = Math.floor(this.next() * (i + 1));
      [out[i], out[j]] = [out[j], out[i]];
    }
    return out;
  }
}

// ------------------------------------------------------------------ map graph
// Approximate positions of Mirage callouts in game units. Players walk along
// the edges between them.

const NODES = {
  t_spawn: [1250, -100],
  t_south: [1150, -750],
  t_ramp: [815, -1400],
  a_ramp: [250, -1650],
  a_tetris: [-200, -1750],
  a_site: [-400, -2050],
  a_default: [-250, -2200],
  a_firebox: [-650, -2250],
  palace_entry: [850, -1950],
  palace: [400, -2250],
  palace_exit: [50, -2250],
  ticket: [-1150, -2050],
  stairs: [-800, -1600],
  jungle: [-1150, -1350],
  connector: [-850, -1150],
  connector_mid: [-650, -900],
  window: [-1250, -750],
  ct_spawn: [-1700, -1950],
  ct_mid: [-1450, -1600],
  ct_north: [-2000, -1100],
  market: [-2300, -550],
  kitchen: [-2350, -100],
  b_site: [-2100, 400],
  b_van: [-2250, 200],
  b_bench: [-2000, 650],
  b_entry: [-1750, 550],
  b_short_exit: [-1650, 150],
  b_short: [-1350, 100],
  catwalk: [-900, -350],
  top_mid: [300, -650],
  mid: [-350, -650],
  underpass_entry: [-750, -250],
  underpass: [-1050, 150],
  apps_bottom: [-1350, 600],
  apps_hall: [-500, 650],
  apps: [300, 650],
  t_apps: [1100, 400],
};

const EDGES = [
  ["t_spawn", "t_south"], ["t_spawn", "t_apps"], ["t_spawn", "top_mid"],
  ["t_south", "t_ramp"], ["t_south", "palace_entry"],
  ["t_ramp", "a_ramp"], ["a_ramp", "a_tetris"], ["a_tetris", "a_site"],
  ["palace_entry", "palace"], ["palace", "palace_exit"], ["palace_exit", "a_default"],
  ["a_default", "a_site"], ["a_site", "a_firebox"], ["a_site", "ticket"], ["a_firebox", "ticket"],
  ["a_site", "stairs"], ["stairs", "jungle"], ["jungle", "connector"], ["connector", "connector_mid"],
  ["connector_mid", "mid"], ["jungle", "window"], ["ticket", "ct_spawn"], ["ct_spawn", "ct_mid"],
  ["ct_mid", "jungle"], ["ct_spawn", "ct_north"], ["ct_north", "market"], ["market", "kitchen"],
  ["kitchen", "b_van"], ["b_van", "b_site"], ["b_site", "b_bench"], ["b_site", "b_entry"],
  ["b_entry", "apps_bottom"], ["b_site", "b_short_exit"], ["b_short_exit", "b_short"],
  ["b_short", "catwalk"], ["catwalk", "mid"], ["top_mid", "mid"], ["mid", "underpass_entry"],
  ["underpass_entry", "underpass"], ["underpass", "apps_bottom"], ["apps_bottom", "apps_hall"],
  ["apps_hall", "apps"], ["apps", "t_apps"],
];

const dist = (a, b) => Math.sqrt((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2);

const ADJ = new Map(Object.keys(NODES).map((n) => [n, []]));
for (const [a, b] of EDGES) {
  const w = dist(NODES[a], NODES[b]);
  ADJ.get(a).push([b, w]);
  ADJ.get(b).push([a, w]);
}

const routeCache = new Map();
function route(from, to) {
  const key = `${from}>${to}`;
  if (routeCache.has(key)) return routeCache.get(key);
  const best = new Map([[from, 0]]);
  const prev = new Map();
  const open = new Set([from]);
  while (open.size) {
    let cur = null;
    for (const n of open) if (cur === null || best.get(n) < best.get(cur)) cur = n;
    open.delete(cur);
    if (cur === to) break;
    for (const [next, w] of ADJ.get(cur)) {
      const d = best.get(cur) + w;
      if (!best.has(next) || d < best.get(next)) {
        best.set(next, d);
        prev.set(next, cur);
        open.add(next);
      }
    }
  }
  const out = [to];
  while (out[0] !== from) out.unshift(prev.get(out[0]));
  routeCache.set(key, out);
  return out;
}

// ------------------------------------------------------------------ the match script

const PLAYERS = [
  // team A starts CT
  { name: "Ashgrove", team: "A" },
  { name: "birch", team: "A" },
  { name: "Cedar.Lane", team: "A" },
  { name: "dogwood_", team: "A" },
  { name: "Elm Street Sniper", team: "A", awper: true },
  { name: "fig", team: "B" },
  { name: "Ginkgo★", team: "B", awper: true },
  { name: "hazel", team: "B" },
  { name: "ivy-league", team: "B" },
  { name: "JUNIPER", team: "B" },
].map((p, idx) => ({ idx, steam_id: (76561199999999001n + BigInt(idx)).toString(), ...p }));

// MR12, A starts CT, final score 13-9. [winner, reason]
const SCRIPT = [
  ["A", "bomb_defused"], ["A", "elimination"], ["B", "bomb_exploded"], ["A", "elimination"],
  ["A", "time_expired"], ["B", "elimination"], ["B", "bomb_exploded"], ["A", "bomb_defused"],
  ["A", "elimination"], ["B", "elimination"], ["A", "elimination"], ["B", "bomb_exploded"],
  // half time: A plays T from round 13
  ["B", "elimination"], ["B", "elimination"], ["A", "bomb_exploded"], ["A", "elimination"],
  ["A", "bomb_exploded"], ["B", "bomb_defused"], ["A", "elimination"], ["B", "time_expired"],
  ["A", "bomb_exploded"], ["A", "elimination"],
];

// Main weapon per buy and side: four riflers, then the AWPer.
const LOADOUTS = {
  pistol: { T: ["glock", "glock", "p250", "glock", "glock"], CT: ["usp_silencer", "hkp2000", "usp_silencer", "p250", "usp_silencer"] },
  eco: { T: ["glock", "tec9", "glock", "p250", "deagle"], CT: ["usp_silencer", "fiveseven", "p250", "usp_silencer", "deagle"] },
  force: { T: ["galilar", "mac10", "deagle", "galilar", "ssg08"], CT: ["famas", "mp9", "deagle", "famas", "ssg08"] },
  smg: { T: ["mac10", "galilar", "ak47", "mac10", "galilar"], CT: ["mp9", "famas", "mp9", "ump45", "famas"] },
  rifle: { T: ["ak47", "ak47", "ak47", "ak47", "awp"], CT: ["m4a1_silencer", "m4a1", "m4a1_silencer", "famas", "awp"] },
};
const SIDEARM = { T: "glock", CT: "usp_silencer" };
const HEADSHOT_RATE = {
  awp: 0.08, ssg08: 0.15, deagle: 0.5, ak47: 0.5, m4a1: 0.45, m4a1_silencer: 0.48, famas: 0.4,
  galilar: 0.42, mac10: 0.35, mp9: 0.35, ump45: 0.38,
};
const headshotRate = (weapon) => HEADSHOT_RATE[weapon] ?? 0.55; // pistols

// T strategies: each role walks to a stage, waits for the execute, then takes
// its spot on site. CT setup: two A, one window, two B.
const T_STRATS = {
  A: [
    { role: "ramp1", stage: "t_ramp", final: "a_tetris" },
    { role: "ramp2", stage: "t_ramp", final: "a_site", planter: true },
    { role: "palace1", stage: "palace", final: "a_default" },
    { role: "palace2", stage: "palace", final: "a_firebox" },
    { role: "mid", stage: "mid", final: "jungle", awp: true },
  ],
  B: [
    { role: "apps1", stage: "apps_hall", final: "b_site", planter: true },
    { role: "apps2", stage: "apps_hall", final: "b_bench" },
    { role: "apps3", stage: "apps_hall", final: "b_van" },
    { role: "short", stage: "catwalk", final: "b_short_exit", awp: true },
    { role: "underpass", stage: "underpass_entry", final: "apps_bottom" },
  ],
};
const T_HOLD_WATCH = { A: "ticket", B: "market" };
const CT_SETUP = [
  { role: "a_anchor", hold: "a_firebox", watch: "palace_exit", site: "A" },
  { role: "jungle", hold: "jungle", watch: "connector_mid", site: "A" },
  { role: "window", hold: "window", watch: "top_mid", site: "mid", awp: true },
  { role: "b_anchor", hold: "b_van", watch: "apps_bottom", site: "B" },
  { role: "b_short", hold: "b_short_exit", watch: "catwalk", site: "B" },
];
const RETAKE = { A: { node: "a_site", watch: "a_ramp" }, B: { node: "b_site", watch: "apps_bottom" } };

// Grenades. `at` is seconds relative to the execute (or absolute with abs: true).
// `tiers` lists the buys that include the grenade.
const BUY = ["force", "smg", "rifle"];
const T_NADES = {
  A: [
    { role: "mid", type: "smoke", at: 4.5, abs: true, to: [-1250, -760], tiers: [...BUY, "pistol"] }, // window
    { role: "ramp1", type: "smoke", at: -5, to: [-1120, -2010], tiers: BUY }, // CT
    { role: "ramp2", type: "smoke", at: -4.5, to: [-1130, -1360], tiers: [...BUY, "pistol"] }, // jungle
    { role: "palace1", type: "smoke", at: -4, to: [-780, -1590], tiers: ["smg", "rifle"] }, // stairs
    { role: "palace2", type: "molotov", at: -1, to: [-640, -2240], tiers: BUY }, // firebox
    { role: "palace1", type: "molotov", at: -0.6, to: [-270, -2180], tiers: ["smg", "rifle"] }, // default
    { role: "ramp1", type: "flash", at: 0.4, to: [-330, -1960], tiers: [...BUY, "pistol", "eco"] },
    { role: "palace1", type: "flash", at: 0.9, to: [-280, -2130], tiers: [...BUY, "pistol"] },
    { role: "ramp2", type: "he", at: 1.6, to: [-420, -2060], tiers: ["smg", "rifle"] },
    { role: "palace2", type: "he", at: 2.2, to: [-600, -2200], tiers: ["rifle"] },
  ],
  B: [
    { role: "short", type: "smoke", at: 4.5, abs: true, to: [-1250, -760], tiers: [...BUY, "pistol"] }, // window
    { role: "apps1", type: "smoke", at: -5, to: [-2280, -360], tiers: BUY }, // market window
    { role: "apps2", type: "smoke", at: -4.5, to: [-2300, -20], tiers: [...BUY, "pistol"] }, // kitchen
    { role: "apps3", type: "molotov", at: -1, to: [-2010, 640], tiers: BUY }, // bench
    { role: "apps1", type: "molotov", at: -0.6, to: [-2230, 210], tiers: ["smg", "rifle"] }, // van
    { role: "apps1", type: "flash", at: 0.4, to: [-1960, 480], tiers: [...BUY, "pistol", "eco"] },
    { role: "short", type: "flash", at: 0.8, to: [-1820, 260], tiers: [...BUY, "pistol"] },
    { role: "apps2", type: "he", at: 1.6, to: [-2230, 230], tiers: ["smg", "rifle"] },
    { role: "apps3", type: "he", at: 2.2, to: [-2080, 380], tiers: ["rifle"] },
    { role: "underpass", type: "decoy", at: 20, abs: true, to: [-1300, 120], tiers: ["rifle"] },
  ],
};
const CT_NADES = {
  any: [
    { role: "window", type: "flash", at: 9, abs: true, to: [120, -640], tiers: ["smg", "rifle"] },
    { role: "jungle", type: "he", at: 12, abs: true, to: [180, -660], tiers: ["rifle"] }, // top mid
    { role: "jungle", type: "decoy", at: 16, abs: true, to: [-520, -760], tiers: ["rifle"] },
    { role: "b_short", type: "incendiary", at: 21, abs: true, to: [-880, -360], tiers: BUY }, // catwalk
  ],
  A: [
    { role: "a_anchor", type: "incendiary", at: 1.2, to: [120, -1700], tiers: BUY }, // ramp
    { role: "jungle", type: "flash", at: 2.5, to: [-180, -1760], tiers: [...BUY, "pistol"] },
    { role: "rotator1", type: "he", at: 13, to: [-380, -2020], tiers: [...BUY, "pistol"] },
    { role: "rotator2", type: "smoke", at: 15, to: [-260, -1820], tiers: ["smg", "rifle"] }, // retake smoke
  ],
  B: [
    { role: "b_anchor", type: "incendiary", at: 1, to: [-1450, 600], tiers: BUY }, // apps bottom
    { role: "b_short", type: "flash", at: 2.5, to: [-1700, 520], tiers: [...BUY, "pistol"] },
    { role: "rotator1", type: "he", at: 14, to: [-2080, 430], tiers: [...BUY, "pistol"] },
    { role: "rotator2", type: "smoke", at: 16, to: [-1720, 560], tiers: ["smg", "rifle"] },
  ],
};
// Seconds from throw to detonation, and from detonation to the end of the effect.
const FUSE_S = { flash: 1.625, he: 1.625 };
const FLIGHT_SPEED = { smoke: 650, molotov: 700, incendiary: 700, decoy: 650 };
const SETTLE_S = { smoke: 0.625, molotov: 0.125, incendiary: 0.125, decoy: 0.5 };
const EFFECT_S = { smoke: 18.125, molotov: 7, incendiary: 7, decoy: 15, flash: 0, he: 0 };

// ------------------------------------------------------------------ helpers

const round1 = (v) => Math.round(v * 10) / 10;
const sideOf = (team, sideA) => (team === "A" ? sideA : sideA === "CT" ? "T" : "CT");
const normDeg = (d) => ((d % 360) + 360) % 360;
const angleTo = (from, to) => normDeg((Math.atan2(to[1] - from[1], to[0] - from[0]) * 180) / Math.PI);
const angleDiff = (a, b) => ((((b - a) % 360) + 540) % 360) - 180; // signed, in (-180, 180]

// history: round n -> winning team, for rounds already played.
function buyTier(n, team, history) {
  const pistol = n <= 12 ? 1 : 13;
  if (n === pistol) return "pistol";
  const lostPistol = history[pistol] !== team;
  if (n === pistol + 1) return lostPistol ? "eco" : "smg";
  if (n === pistol + 2 && lostPistol && history[pistol + 1] !== team) return "force";
  return "rifle";
}

// ------------------------------------------------------------------ one round

function simulateRound(n, winner, reason, history, startTick) {
  const rng = new Rng(SEED + n * 7919);
  const sideA = n <= 12 ? "CT" : "T";
  const freezeEndTick = startTick + FREEZE_S * TICK_RATE;
  const teamOn = { CT: sideA === "CT" ? "A" : "B", T: sideA === "CT" ? "B" : "A" };
  const winSide = sideOf(winner, sideA);
  const tiers = { CT: buyTier(n, teamOn.CT, history), T: buyTier(n, teamOn.T, history) };

  // Roles: the AWPer takes the AWP role; the rest are shuffled once per half.
  const site = rng.chance(0.5) ? "A" : "B";
  const executes = reason !== "time_expired";
  const halfRng = (team) => new Rng(SEED + (n <= 12 ? 1 : 2) * 104729 + (team === "A" ? 0 : 1299709));
  const members = (side) => PLAYERS.filter((p) => p.team === teamOn[side]);
  function assign(side, roles) {
    const ps = members(side);
    const awper = ps.find((p) => p.awper);
    const others = halfRng(teamOn[side]).shuffle(ps.filter((p) => p !== awper));
    const nonAwpRoles = roles.filter((r) => !r.awp);
    const out = new Map();
    out.set(awper.idx, roles.find((r) => r.awp));
    others.forEach((p, i) => out.set(p.idx, nonAwpRoles[i]));
    return out;
  }
  const tRoles = assign("T", T_STRATS[site]);
  const ctRoles = assign("CT", CT_SETUP);

  // Execute time, in seconds after freeze end.
  const E = !executes
    ? Infinity
    : tiers.T === "pistol" ? rng.float(34, 46) : tiers.T === "eco" ? rng.float(42, 54) : rng.float(36, 56);
  const rotateAt = E + rng.float(3, 7);
  const rotators = [...ctRoles].filter(([, r]) => r.site !== site).map(([idx]) => idx);

  // Plans and movement, ignoring deaths (a dead player's track is cut later).
  const tracks = new Map();
  for (const p of PLAYERS) {
    const side = sideOf(p.team, sideA);
    const offset = [rng.float(-40, 40), rng.float(-40, 40)];
    const spot = [rng.float(-70, 70), rng.float(-70, 70)];
    const tier = tiers[side];
    const speed = (p.awper && tier === "rifle" ? 205 : 228) + (tier === "pistol" || tier === "eco" ? 12 : 0) + rng.float(-8, 8);
    let start, startNode, steps;
    if (side === "T") {
      const role = tRoles.get(p.idx);
      startNode = "t_spawn";
      start = [NODES.t_spawn[0] + rng.float(-180, 160), NODES.t_spawn[1] + rng.float(-170, 170)];
      const toward = route(role.stage, role.final)[1] ?? role.final;
      steps = [
        { hold: rng.float(0, 1.2) },
        { go: role.stage },
        { hold: E + rng.float(0, 2.5), watch: toward },
        { go: role.final, final: true },
        { hold: Infinity, watch: T_HOLD_WATCH[site] },
      ];
    } else {
      const role = ctRoles.get(p.idx);
      startNode = "ct_spawn";
      start = [NODES.ct_spawn[0] + rng.float(-140, 140), NODES.ct_spawn[1] + rng.float(-110, 110)];
      steps = [
        { hold: rng.float(0, 1) },
        { go: role.hold, final: role.site === site || !executes },
        { hold: rotators.includes(p.idx) && executes ? rotateAt + rng.float(0, 2) : Infinity, watch: role.watch },
        { go: RETAKE[site].node, final: true },
        { hold: Infinity, watch: RETAKE[site].watch },
      ];
    }
    tracks.set(p.idx, { idx: p.idx, side, ...walk(startNode, start, steps, offset, spot, speed) });
  }

  // Kill budget for the scripted outcome.
  const budget = { T: 0, CT: 0 }; // how many players of each side die
  const losers = winSide === "CT" ? "T" : "CT";
  const smallLoss = () => rng.weighted({ 0: 1, 1: 3, 2: 4, 3: 3, 4: 2 });
  if (reason === "elimination") {
    budget[losers] = 5;
    budget[winSide] = smallLoss();
  } else if (reason === "bomb_exploded") {
    budget.CT = rng.int(3, 4);
    budget.T = rng.int(1, 4);
  } else if (reason === "bomb_defused") {
    budget.T = rng.chance(0.8) ? 5 : 4;
    budget.CT = rng.int(1, 4);
  } else {
    budget.T = rng.int(2, 4);
    budget.CT = rng.int(1, 3);
  }

  // Bomb plant: the planter reaches the site, then takes 3.2 s.
  const planted = reason === "bomb_exploded" || reason === "bomb_defused";
  const planter = planted ? [...tRoles].find(([, r]) => r.planter)[0] : null;
  const plantS = planted ? tracks.get(planter).arriveFrame / HZ + 3.2 + rng.float(0.3, 2) : Infinity;
  const deadlineS = { elimination: 108, bomb_exploded: plantS + 36, bomb_defused: plantS + 28, time_expired: 112 }[reason];

  const kills = scheduleKills(rng, tracks, budget, { planter, plantFrame: Math.ceil(plantS * HZ), deadlineFrame: Math.floor(deadlineS * HZ) });
  for (const k of kills) {
    // Any tick in (frame - 1, frame] maps to `frame`: the first frame the victim is dead.
    k.tick = freezeEndTick + k.frame * TICKS_PER_FRAME - k.subTick;
    const side = tracks.get(k.attacker).side;
    const shooter = PLAYERS[k.attacker];
    const slot = shooter.awper ? 4 : members(side).filter((p) => !p.awper).indexOf(shooter);
    const main = LOADOUTS[tiers[side]][side][slot];
    k.weapon = tiers[side] !== "pistol" && tiers[side] !== "eco" && rng.chance(0.08) ? SIDEARM[side] : main;
    k.headshot = rng.chance(headshotRate(k.weapon));
  }

  // Round end.
  const lastKillS = kills.length ? kills[kills.length - 1].frame / HZ : 0;
  let endTick;
  if (reason === "elimination") {
    endTick = kills[kills.length - 1].tick;
  } else if (reason === "bomb_exploded") {
    endTick = freezeEndTick + Math.round((plantS + BOMB_TIMER_S) * TICK_RATE);
  } else if (reason === "bomb_defused") {
    const defuseDone = Math.min(Math.max(lastKillS + rng.float(2, 4), plantS + rng.float(8, 14)) + 5, plantS + 38.5);
    endTick = freezeEndTick + Math.round(defuseDone * TICK_RATE);
  } else {
    endTick = freezeEndTick + ROUND_TIME_S * TICK_RATE;
  }
  const frames = Math.ceil((endTick - freezeEndTick) / TICKS_PER_FRAME) + POST_ROUND_S * HZ;

  // Grenades: thrown if the thrower is alive and the grenade detonates in time.
  const deathFrame = new Map(PLAYERS.map((p) => [p.idx, Infinity]));
  for (const k of kills) deathFrame.set(k.victim, k.frame);
  const roleHolder = new Map();
  for (const [idx, r] of tRoles) roleHolder.set(`T:${r.role}`, idx);
  for (const [idx, r] of ctRoles) roleHolder.set(`CT:${r.role}`, idx);
  rotators.forEach((idx, i) => roleHolder.set(`CT:rotator${i + 1}`, idx));
  const nadeSpecs = [
    ...T_NADES[site].map((g) => ({ ...g, side: "T" })),
    ...CT_NADES.any.map((g) => ({ ...g, side: "CT" })),
    ...(executes ? CT_NADES[site].map((g) => ({ ...g, side: "CT", at: g.at + (g.role.startsWith("rotator") ? rotateAt - E : 0) })) : []),
  ];
  const grenades = [];
  for (const g of nadeSpecs) {
    if (!g.tiers.includes(tiers[g.side])) continue;
    const thrower = roleHolder.get(`${g.side}:${g.role}`);
    const throwS = g.abs ? g.at : E + g.at;
    if (thrower === undefined || !Number.isFinite(throwS)) continue;
    const throwFrame = Math.round(throwS * HZ);
    if (throwFrame >= deathFrame.get(thrower)) continue;
    const t = tracks.get(thrower);
    const from = [t.x[throwFrame], t.y[throwFrame]];
    const to = [g.to[0] + rng.float(-25, 25), g.to[1] + rng.float(-25, 25)];
    const flight = g.type in FUSE_S ? Math.round(FUSE_S[g.type] * HZ) : Math.max(HZ / 2, Math.ceil((dist(from, to) / FLIGHT_SPEED[g.type]) * HZ) + Math.round(SETTLE_S[g.type] * HZ));
    const detonateFrame = throwFrame + flight;
    if (detonateFrame >= frames) continue;
    const pathPts = [];
    for (let i = 0; i <= flight; i++) {
      const s = 1 - (1 - i / flight) ** 2; // slows down as it lands
      pathPts.push([Math.round(from[0] + (to[0] - from[0]) * s), Math.round(from[1] + (to[1] - from[1]) * s)]);
    }
    grenades.push({
      type: g.type,
      thrower,
      throw_frame: throwFrame,
      detonate_frame: detonateFrame,
      end_frame: Math.min(detonateFrame + Math.round(EFFECT_S[g.type] * HZ), frames - 1),
      path: pathPts,
      _yawTarget: to,
    });
  }
  grenades.sort((a, b) => a.throw_frame - b.throw_frame || a.thrower - b.thrower);

  // Damage: lethal hits, earlier non-lethal hits (some by a teammate, which can
  // earn an assist), HE damage, and chip damage on survivors.
  const at = (idx, f) => [tracks.get(idx).x[f], tracks.get(idx).y[f]];
  const aliveAt = (idx, f) => deathFrame.get(idx) > f;
  const hits = [];
  for (const k of kills) {
    hits.push({ tick: k.tick, attacker: k.attacker, victim: k.victim, lethal: true });
    if (!rng.chance(k.weapon === "awp" ? 0.15 : 0.6)) continue;
    const hitFrame = Math.max(1, k.frame - rng.int(3, 40));
    const mates = PLAYERS.filter(
      (p) => p.team === PLAYERS[k.attacker].team && p.idx !== k.attacker && aliveAt(p.idx, hitFrame) && dist(at(p.idx, hitFrame), at(k.victim, hitFrame)) < 1600,
    );
    const hitter = mates.length && rng.chance(0.75) ? rng.pick(mates).idx : k.attacker;
    hits.push({ tick: freezeEndTick + hitFrame * TICKS_PER_FRAME - rng.int(0, 3), attacker: hitter, victim: k.victim, amount: rng.int(22, 90) });
  }
  for (const g of grenades.filter((g) => g.type === "he")) {
    const blast = g.path[g.path.length - 1];
    for (const p of PLAYERS) {
      if (sideOf(p.team, sideA) === tracks.get(g.thrower).side || !aliveAt(p.idx, g.detonate_frame)) continue;
      const d = dist(blast, at(p.idx, g.detonate_frame));
      if (d < 350) {
        hits.push({ tick: freezeEndTick + g.detonate_frame * TICKS_PER_FRAME, attacker: g.thrower, victim: p.idx, amount: Math.max(1, Math.round(60 * (1 - d / 350))) });
      }
    }
  }
  for (const p of PLAYERS) {
    if (deathFrame.get(p.idx) !== Infinity || !rng.chance(0.35)) continue;
    const mySide = tracks.get(p.idx).side;
    for (let f = rng.int(10 * HZ, 25 * HZ); f < frames - POST_ROUND_S * HZ; f += HZ / 2) {
      const enemy = PLAYERS.find((e) => tracks.get(e.idx).side !== mySide && aliveAt(e.idx, f) && dist(at(e.idx, f), at(p.idx, f)) < 1200);
      if (enemy) {
        hits.push({ tick: freezeEndTick + f * TICKS_PER_FRAME - rng.int(0, 3), attacker: enemy.idx, victim: p.idx, amount: rng.int(8, 70) });
        break;
      }
    }
  }
  hits.sort((a, b) => a.tick - b.tick || (a.lethal ? 1 : 0) - (b.lethal ? 1 : 0));
  const hp = new Map(PLAYERS.map((p) => [p.idx, 100]));
  const damage = []; // applied hits: { tick, attacker, victim, amount }
  for (const h of hits) {
    const cur = hp.get(h.victim);
    if (cur <= 0) continue;
    const amount = h.lethal ? cur : Math.min(h.amount, cur - 1);
    if (amount <= 0) continue;
    hp.set(h.victim, cur - amount);
    damage.push({ tick: h.tick, attacker: h.attacker, victim: h.victim, amount });
  }
  for (const k of kills) {
    const dealt = new Map();
    for (const d of damage) {
      if (d.victim === k.victim && d.tick < k.tick && d.attacker !== k.attacker && PLAYERS[d.attacker].team === PLAYERS[k.attacker].team) {
        dealt.set(d.attacker, (dealt.get(d.attacker) ?? 0) + d.amount);
      }
    }
    const best = [...dealt].filter(([, dmg]) => dmg > 40).sort((a, b) => b[1] - a[1] || a[0] - b[0])[0];
    k.assister = best ? best[0] : null;
  }

  const roundMeta = { n, start_tick: startTick, freeze_end_tick: freezeEndTick, end_tick: endTick, winner, reason, side_a: sideA };
  return { roundMeta, kills, damage, frames, freezeEndTick, tracks, deathFrame, grenades };
}

// Walks a plan of { go: node } and { hold: untilSeconds, watch: node } steps.
function walk(startNode, start, steps, offset, spot, speed) {
  const x = new Float64Array(SIM_FRAMES);
  const y = new Float64Array(SIM_FRAMES);
  const yawTarget = new Float64Array(SIM_FRAMES);
  let pos = [...start];
  let node = startNode;
  let si = 0;
  let queue = null;
  let arriveFrame = -1;
  let lastYaw = angleTo(start, NODES[steps.find((s) => s.go).go]);
  for (let f = 0; f < SIM_FRAMES; f++) {
    const t = f / HZ;
    let budget = speed / HZ;
    let moved = [0, 0];
    let watch = null;
    for (;;) {
      const step = steps[si];
      if (step.hold !== undefined) {
        if (t >= step.hold) {
          si++;
          continue;
        }
        watch = step.watch ? NODES[step.watch] : null;
        break;
      }
      if (!queue) {
        const names = route(node, step.go).slice(1);
        queue = names.map((name, i) => {
          const extra = step.final && i === names.length - 1 ? spot : [0, 0];
          return [NODES[name][0] + offset[0] + extra[0], NODES[name][1] + offset[1] + extra[1]];
        });
      }
      while (budget > 1e-9 && queue.length) {
        const d = dist(pos, queue[0]);
        const s = Math.min(1, budget / d || 1);
        const nx = pos[0] + (queue[0][0] - pos[0]) * s;
        const ny = pos[1] + (queue[0][1] - pos[1]) * s;
        moved = [moved[0] + nx - pos[0], moved[1] + ny - pos[1]];
        budget -= Math.min(budget, d);
        pos = [nx, ny];
        if (s >= 1) queue.shift();
      }
      if (queue.length === 0) {
        node = step.go;
        queue = null;
        si++;
        if (step.final && arriveFrame < 0) arriveFrame = f;
        continue;
      }
      break;
    }
    x[f] = pos[0];
    y[f] = pos[1];
    if (Math.abs(moved[0]) + Math.abs(moved[1]) > 0.5) lastYaw = angleTo([0, 0], moved);
    else if (watch) lastYaw = angleTo(pos, watch);
    yawTarget[f] = lastYaw;
  }
  return { x, y, yawTarget, arriveFrame };
}

// Places kills where an alive T and an alive CT are close, until each side has
// lost the scripted number of players.
function scheduleKills(rng, tracks, budget, { planter, plantFrame, deadlineFrame }) {
  const alive = new Map([...tracks.keys()].map((idx) => [idx, true]));
  const ts = [...tracks.values()].filter((t) => t.side === "T");
  const cts = [...tracks.values()].filter((t) => t.side === "CT");
  const left = { ...budget }; // deaths still owed per side
  const aliveCount = { T: ts.length, CT: cts.length };
  // A side may lose a player if it still owes deaths, and may not lose its last
  // player while the other side still owes deaths (the round would be over).
  const canLose = (side, other) => left[side] > 0 && (aliveCount[side] > 1 || left[other] === 0);
  const kills = [];
  let cursor = Math.round(rng.float(8, 18) * HZ);
  let range = 1100;
  for (let guard = 0; guard < 200 && (left.T > 0 || left.CT > 0); guard++) {
    let found = null;
    for (let f = cursor; f <= deadlineFrame && !found; f++) {
      if (range < 5000 && !rng.chance(DUELS_PER_S / HZ)) continue;
      const pairs = [];
      for (const t of ts) {
        if (!alive.get(t.idx)) continue;
        for (const ct of cts) {
          if (!alive.get(ct.idx)) continue;
          const d = dist([t.x[f], t.y[f]], [ct.x[f], ct.y[f]]);
          const tCanWin = canLose("CT", "T");
          const ctCanWin = canLose("T", "CT") && !(t.idx === planter && f < plantFrame);
          if (d < range && (tCanWin || ctCanWin)) pairs.push({ t, ct, d, tCanWin, ctCanWin });
        }
      }
      if (pairs.length) {
        // Closer pairs are likelier to fight, but not always the closest.
        const weights = pairs.map((p) => 1 / (p.d + 250));
        let r = rng.float(0, weights.reduce((a, b) => a + b, 0));
        found = { f, ...pairs.find((_, i) => (r -= weights[i]) < 0 || i === pairs.length - 1) };
      }
    }
    if (!found) {
      range *= 1.6;
      cursor = kills.length ? Math.min(kills[kills.length - 1].frame + HZ / 2, deadlineFrame) : cursor;
      continue;
    }
    const { f, t, ct, tCanWin, ctCanWin } = found;
    // AWPers win more of their duels.
    let pT = left.CT / (left.CT + left.T);
    if (PLAYERS[t.idx].awper) pT = Math.sqrt(pT);
    if (PLAYERS[ct.idx].awper) pT = pT * pT;
    const tWins = tCanWin && (!ctCanWin || rng.chance(pT));
    const [attacker, victim] = tWins ? [t, ct] : [ct, t];
    left[victim.side]--;
    aliveCount[victim.side]--;
    alive.set(victim.idx, false);
    kills.push({ frame: f, subTick: rng.int(0, TICKS_PER_FRAME - 1), attacker: attacker.idx, victim: victim.idx });
    cursor = f + (rng.chance(0.35) ? rng.int(4, 12) : rng.int(16, 90));
  }
  if (left.T > 0 || left.CT > 0) throw new Error(`could not place every scripted kill (still owed: ${JSON.stringify(left)})`);
  return kills;
}

// ------------------------------------------------------------------ round file

function buildRoundFile(sim) {
  const { roundMeta, frames, freezeEndTick, tracks, deathFrame, grenades, kills } = sim;
  // Yaw: steer toward the movement/watch direction, face the victim before a
  // kill and the landing spot before a throw, then rate-limit the turning.
  const overrides = new Map([...tracks.keys()].map((idx) => [idx, []]));
  for (const k of kills) {
    overrides.get(k.attacker).push({ from: k.frame - 8, to: k.frame, target: k.victim });
    overrides.get(k.victim).push({ from: k.frame - 4, to: k.frame, target: k.attacker });
  }
  for (const g of grenades) overrides.get(g.thrower).push({ from: g.throw_frame - 6, to: g.throw_frame, point: g._yawTarget });

  const players = [];
  for (const t of [...tracks.values()].sort((a, b) => a.idx - b.idx)) {
    const dead = deathFrame.get(t.idx);
    const x = [], y = [], yaw = [], hp = [];
    let cur = t.yawTarget[0];
    let health = 100;
    const myDamage = sim.damage.filter((d) => d.victim === t.idx);
    let di = 0;
    for (let f = 0; f < frames; f++) {
      const g = Math.min(f, dead); // hold the death frame's position
      if (f <= dead) {
        let target = t.yawTarget[f];
        for (const o of overrides.get(t.idx)) {
          if (f >= o.from && f <= o.to) {
            const other = o.point ?? [tracks.get(o.target).x[f], tracks.get(o.target).y[f]];
            target = angleTo([t.x[f], t.y[f]], other);
          }
        }
        const step = angleDiff(cur, target);
        cur = normDeg(cur + Math.max(-30, Math.min(30, step)));
      }
      const tick = freezeEndTick + f * TICKS_PER_FRAME;
      while (di < myDamage.length && myDamage[di].tick <= tick) health -= myDamage[di++].amount;
      x.push(Math.round(t.x[g]));
      y.push(Math.round(t.y[g]));
      yaw.push(Math.round(cur) % 360);
      hp.push(health);
    }
    players.push({ idx: t.idx, x, y, yaw, hp });
  }
  return {
    schema_version: 1,
    round: roundMeta.n,
    hz: HZ,
    start_tick: freezeEndTick,
    frames,
    players,
    grenades: grenades.map(({ _yawTarget, ...g }) => g),
    events: kills.map((k) => ({ frame: k.frame, type: "kill", kill: k.globalIndex })),
  };
}

// ------------------------------------------------------------------ self-checks

function check(cond, msg) {
  if (!cond) throw new Error(`self-check failed: ${msg}`);
}

function selfCheck(rf) {
  for (const p of rf.players) {
    for (let f = 0; f < rf.frames; f++) {
      check(p.x[f] >= RADAR.minX && p.x[f] <= RADAR.maxX && p.y[f] >= RADAR.minY && p.y[f] <= RADAR.maxY, `round ${rf.round} idx ${p.idx} off the radar at ${f}`);
      if (f > 0) {
        check(p.hp[f] <= p.hp[f - 1], `round ${rf.round} idx ${p.idx} heals at ${f}`);
        check(Math.sqrt((p.x[f] - p.x[f - 1]) ** 2 + (p.y[f] - p.y[f - 1]) ** 2) <= 20, `round ${rf.round} idx ${p.idx} jumps at ${f}`);
      }
    }
  }
}

// ------------------------------------------------------------------ output

// JSON with chosen containers expanded one entry per line and the rest inline.
function format(value, expand, at = [], indent = "") {
  const inline = (v) => {
    if (Array.isArray(v)) return `[${v.map(inline).join(",")}]`;
    if (v && typeof v === "object") return `{${Object.entries(v).map(([k, x]) => `${JSON.stringify(k)}: ${inline(x)}`).join(", ")}}`;
    return JSON.stringify(v);
  };
  if (value === null || typeof value !== "object" || !expand(at, value)) return inline(value);
  const inner = indent + "  ";
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    return `[\n${value.map((v, i) => inner + format(v, expand, [...at, i], inner)).join(",\n")}\n${indent}]`;
  }
  const body = Object.entries(value).map(([k, v]) => `${inner}${JSON.stringify(k)}: ${format(v, expand, [...at, k], inner)}`);
  return `{\n${body.join(",\n")}\n${indent}}`;
}

function writeJson(file, value, expand) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, format(value, expand) + "\n");
  return fs.statSync(file).size;
}

// ------------------------------------------------------------------ main

function main() {
  const history = {}; // n -> winning team
  const sims = [];
  let tick = FIRST_ROUND_START_TICK;
  SCRIPT.forEach(([winner, reason], i) => {
    const n = i + 1;
    const sim = simulateRound(n, winner, reason, history, tick);
    history[n] = winner;
    sims.push(sim);
    tick = sim.roundMeta.end_tick + POST_ROUND_S * TICK_RATE + (n === 12 ? HALFTIME_S * TICK_RATE : 0);
  });

  const kills = [];
  for (const sim of sims) {
    for (const k of sim.kills) {
      k.globalIndex = kills.length;
      kills.push({ round: sim.roundMeta.n, tick: k.tick, attacker: k.attacker, victim: k.victim, assister: k.assister, weapon: k.weapon, headshot: k.headshot });
    }
  }
  const damageBy = new Map(PLAYERS.map((p) => [p.idx, 0]));
  for (const sim of sims) for (const d of sim.damage) damageBy.set(d.attacker, damageBy.get(d.attacker) + d.amount);
  const scoreboard = PLAYERS.map((p) => {
    const mine = kills.filter((k) => k.attacker === p.idx);
    const hs = mine.filter((k) => k.headshot).length;
    return {
      player: p.idx,
      kills: mine.length,
      deaths: kills.filter((k) => k.victim === p.idx).length,
      assists: kills.filter((k) => k.assister === p.idx).length,
      adr: round1(damageBy.get(p.idx) / sims.length),
      hs_pct: mine.length ? round1((100 * hs) / mine.length) : 0,
    };
  });

  const match = {
    schema_version: 1,
    parser_version: "0.0.0-synthetic",
    match: {
      map: "de_mirage",
      tick_rate: TICK_RATE,
      demo_sha256: crypto.createHash("sha256").update("demoView synthetic sample match, not a real demo").digest("hex"),
    },
    players: PLAYERS.map(({ idx, steam_id, name, team }) => ({ idx, steam_id, name, team })),
    rounds: sims.map((s) => s.roundMeta),
    kills,
    scoreboard,
  };

  const sizes = [];
  sizes.push(["match.json", writeJson(path.join(OUT_DIR, "match.json"), match, (at) => at.length <= 1)]);
  const roundsDir = path.join(OUT_DIR, "rounds");
  fs.rmSync(roundsDir, { recursive: true, force: true });
  for (const n of ROUND_FILES) {
    const rf = buildRoundFile(sims[n - 1]);
    selfCheck(rf);
    const name = `${String(n).padStart(2, "0")}.json`;
    const expand = (at) => at.length === 0 || (at.length === 1 && Array.isArray(rf[at[0]])) || (at[0] === "players" && at.length === 2);
    sizes.push([`rounds/${name}`, writeJson(path.join(roundsDir, name), rf, expand), rf.frames, rf.grenades.length]);
  }

  const score = { A: 0, B: 0 };
  for (const s of sims) score[s.roundMeta.winner]++;
  console.log(`de_mirage ${score.A}-${score.B}, ${sims.length} rounds, ${kills.length} kills`);
  for (const [name, bytes, frames, nades] of sizes) {
    console.log(`  ${name.padEnd(16)} ${(bytes / 1024).toFixed(1).padStart(6)} KB${frames ? `  ${frames} frames, ${nades} grenades` : ""}`);
  }
}

main();
