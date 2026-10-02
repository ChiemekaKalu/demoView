// The Pulse eval (.claude/ideas/jev/ci-eval.md): asks Jev every golden state
// from the two public fixtures, scores the answers against the real round
// results, and fails when a metric crosses its threshold.
//
//   CLOUDFLARE_JEV=... CLOUDFLARE_ACCOUNT_ID=... npm run pulse:eval
//
// Writes pulse-eval.json (metrics, model, ledger, every answer by state sha)
// and, under GitHub Actions, a summary table. Without a token it skips with a
// notice, so forks and secretless runs stay green. About $0.01 a run.

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { type Answers, type JevClient, jevClient, JevError, Ledger } from "../src/pulse/jev";
import {
  calibrate,
  checkedHeadline,
  fallbackPCt,
  type Moment,
  MOMENT_QUESTIONS,
  type PulseRoundOut,
  pulseFile,
  ROUND_QUESTIONS,
  type RoundFacts,
  swingTick,
} from "../src/pulse/model";
import { type Side, STATE_VERSION } from "../src/pulse/state";

const FIXTURES = ["valve_mm_mirage_2024", "faceit_nuke_2026"];
const GOLDEN = join("test", "pulse-golden"); // npm scripts run in worker/
const CANARIES = 10;

// From the measured public-fixture numbers, with room for noise. Headline
// precision is per state version: the experiments measured 60% with
// state_version 1's kills-only states, and 80% once states carry economy and
// the bomb timeline, which is where ci-eval.md's 75% comes from.
const BARS = {
  brier: { max: 0.16 },
  auc: { min: 0.85 },
  ece: { max: 0.1 },
  how_won: { min: 0.95 },
  headline_precision: { min: STATE_VERSION === 1 ? 0.55 : 0.75 },
  fallback_brier: { max: 0.17 },
};

// How far a canary's two asks may differ: the answers themselves, not the
// probability vectors behind them. Jev jitters a little between asks (up to
// 0.02 on a noul and 0.12 on a 0-3 score on 2026-10-02), so this is about
// 2.5 times that: room for noise, not for a different model.
const CANARY_TOLERANCE = { noul: 0.05, score: 0.3 };

interface Golden {
  fixture: string;
  match: { rounds: { n: number }[] };
  moments: { round: number; tick: number; sec: number; alive: Record<Side, number>; ct_won: boolean; state: unknown }[];
  rounds: { n: number; facts: RoundFacts & { flawless: boolean; oneVOne: boolean }; state: unknown }[];
}

const token = process.env.CLOUDFLARE_JEV;
const account = process.env.CLOUDFLARE_ACCOUNT_ID;
if (!token || !account) {
  console.log("::notice title=pulse eval skipped::Set CLOUDFLARE_JEV and CLOUDFLARE_ACCOUNT_ID to run it.");
  process.exit(0);
}

// The REST endpoint, shaped like the Worker's AI binding so the eval asks
// through the same client the Pulse consumer uses.
const restAi = {
  async run(model: string, input: unknown): Promise<unknown> {
    const res = await fetch(`https://api.cloudflare.com/client/v4/accounts/${account}/ai/run`, {
      method: "POST",
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: JSON.stringify({ model, input }),
    });
    const body = (await res.json().catch(() => ({}))) as { result?: unknown; errors?: unknown };
    if (!res.ok) throw new Error(`http ${res.status}: ${JSON.stringify(body.errors ?? null)}`);
    return body.result;
  },
} as unknown as Ai;

const ledger = new Ledger();
const jev = jevClient(restAi, "default", ledger);
const goldens: Golden[] = FIXTURES.map((f) => JSON.parse(readFileSync(join(GOLDEN, `${f}.json`), "utf8")));
const expectedModel = JSON.parse(readFileSync(join(GOLDEN, "model.json"), "utf8")).model as string;
const answers: Record<string, Answers> = {};
const models = new Set<string>();
let failed = 0;

const sha = (state: unknown) => createHash("sha256").update(JSON.stringify(state)).digest("hex").slice(0, 12);

async function ask(client: JevClient, state: unknown, questions: typeof MOMENT_QUESTIONS): Promise<Answers | null> {
  try {
    const out = await client.ask(state, questions);
    models.add(out.model);
    return out.answers;
  } catch (err) {
    if (err instanceof JevError && err.kind === "credits") {
      console.log("::error title=pulse eval::AI Gateway credits are empty; top up in the dashboard.");
      process.exit(1);
    }
    failed++;
    return null;
  }
}

// Every moment and every round, then the canaries asked twice more.
const momentAnswers = await Promise.all(
  goldens.flatMap((g) => g.moments.map((m) => ask(jev, m.state, MOMENT_QUESTIONS))),
);
const roundAnswers = await Promise.all(goldens.flatMap((g) => g.rounds.map((r) => ask(jev, r.state, ROUND_QUESTIONS))));
const canaryStates = goldens[0]!.moments.slice(0, CANARIES).map((m) => m.state);
const canaryA = await Promise.all(canaryStates.map((s) => ask(jev, s, MOMENT_QUESTIONS)));
const canaryB = await Promise.all(canaryStates.map((s) => ask(jev, s, MOMENT_QUESTIONS)));

const moments = goldens.flatMap((g) => g.moments);
const rounds = goldens.flatMap((g) => g.rounds);
moments.forEach((m, i) => momentAnswers[i] && (answers[sha(m.state)] = momentAnswers[i]!));
rounds.forEach((r, i) => roundAnswers[i] && (answers[sha(r.state)] = roundAnswers[i]!));

// Win probability, after the deployed temperature, and the fallback's.
const scored = moments.flatMap((m, i) => {
  const a = momentAnswers[i]?.ct_wins;
  return a?.type === "noul" ? [{ p: calibrate(a.noul), y: m.ct_won ? 1 : 0 }] : [];
});
const fallback = moments.map((m) => ({ p: fallbackPCt(m.alive, m.sec), y: m.ct_won ? 1 : 0 }));

// Rounds: how_won against code, and headlines that a check in code can test.
let howWonAgreed = 0;
let headlinesTested = 0;
let headlinesRight = 0;
rounds.forEach((r, i) => {
  const a = roundAnswers[i];
  if (a?.how_won?.type === "choice" && a.how_won.choice === r.facts.how_won) howWonAgreed++;
  const h = a?.headline;
  if (h?.type === "choice" && !["awp_show", "trade_war", "fast_win", "eco_upset"].includes(h.choice)) {
    headlinesTested++;
    if (checkedHeadline(h.choice, r.facts)) headlinesRight++;
  }
});

const metrics = {
  brier: round3(brier(scored)),
  auc: round3(auc(scored)),
  ece: round3(ece(scored)),
  how_won: round3(howWonAgreed / rounds.length),
  headline_precision: round3(headlinesTested ? headlinesRight / headlinesTested : 1),
  fallback_brier: round3(brier(fallback)),
};
const canaryDeltas = canaryA.map((a, i) => answerDelta(a, canaryB[i]));
const canaryDiffers = canaryDeltas.filter(
  (d) => d.noul > CANARY_TOLERANCE.noul || d.score > CANARY_TOLERANCE.score || d.choice,
).length;
const schemaOk = validatePulse();
const callFailures = failed / Math.max(1, ledger.calls + failed);
const model = [...models].join(", ") || "none";

const breaches = Object.entries(BARS).flatMap(([name, bar]) => {
  const v = metrics[name as keyof typeof metrics];
  return ("max" in bar && v > bar.max) || ("min" in bar && v < bar.min) ? [name] : [];
});
if (!schemaOk) breaches.push("schema");
if (canaryDiffers) breaches.push("canary determinism");
if (callFailures > 0.02) breaches.push("call failures");

const report = {
  model,
  expected_model: expectedModel,
  metrics,
  canary_differs: canaryDiffers,
  canary_deltas: canaryDeltas,
  schema_ok: schemaOk,
  ledger: {
    calls: ledger.calls,
    failed,
    retries: ledger.retries,
    http_429: ledger.rateLimited,
    in_tokens: ledger.inTokens,
    usd: Math.round(ledger.usd * 1e4) / 1e4,
    p50_ms: ledger.percentile(0.5),
    p95_ms: ledger.percentile(0.95),
  },
  breaches,
  answers,
};
writeFileSync("pulse-eval.json", JSON.stringify(report, null, 1));

const rows = Object.entries(BARS).map(([name, bar]) => {
  const limit = "max" in bar ? `≤ ${bar.max}` : `≥ ${bar.min}`;
  return `| ${name} | ${metrics[name as keyof typeof metrics]} | ${limit} |`;
});
const summary = [
  `### Pulse eval: ${breaches.length ? `failed (${breaches.join(", ")})` : "passed"}`,
  "",
  "| Metric | Value | Bar |",
  "| --- | --- | --- |",
  ...rows,
  `| schema | ${schemaOk ? "ok" : "fails"} | valid |`,
  `| canaries that differ beyond tolerance | ${canaryDiffers} | 0 |`,
  `| largest canary gap (noul, score) | ${Math.max(0, ...canaryDeltas.map((d) => d.noul))}, ${Math.max(0, ...canaryDeltas.map((d) => d.score))} | ${CANARY_TOLERANCE.noul}, ${CANARY_TOLERANCE.score} |`,
  "",
  `Model ${model}. ${ledger.calls} calls, ${failed} failed, ${ledger.rateLimited} rate-limited, ${ledger.inTokens} input tokens, $${report.ledger.usd}.`,
].join("\n");
console.log(summary);
if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${summary}\n`);
if (!models.has(expectedModel)) {
  console.log(`::warning title=Jev model changed::Answers came from ${model}, the goldens from ${expectedModel}. Re-baseline test/pulse-golden/model.json once the numbers look right.`);
}
process.exit(breaches.length ? 1 : 0);

// The largest gaps between two asks of one state: a noul's probability, a
// score, and whether a choice changed.
function answerDelta(a: Answers | null, b: Answers | null | undefined) {
  const d = { noul: 0, score: 0, choice: false };
  if (!a || !b) return { ...d, choice: true };
  for (const [name, x] of Object.entries(a)) {
    const y = b[name];
    if (x.type === "noul" && y?.type === "noul") d.noul = Math.max(d.noul, round3(Math.abs(x.noul - y.noul)));
    else if (x.type === "score" && y?.type === "score") d.score = Math.max(d.score, round3(Math.abs(x.score - y.score)));
    else if (x.type === "choice" && y?.type === "choice") d.choice ||= x.choice !== y.choice;
    else d.choice = true;
  }
  return d;
}

// Assembles each fixture's pulse.json from the answers and validates it.
function validatePulse(): boolean {
  let i = 0;
  let j = 0;
  const dir = mkdtempSync(join(tmpdir(), "pulse-eval-"));
  for (const g of goldens) {
    const out: PulseRoundOut[] = g.rounds.map((r) => {
      const ms: Moment[] = [];
      for (const m of g.moments.filter((m) => m.round === r.n)) {
        const a = momentAnswers[i++];
        const raw = a?.ct_wins?.type === "noul" ? a.ct_wins.noul : 0.5;
        const tension = a?.tension?.type === "score" ? a.tension.score : 0;
        ms.push({ tick: m.tick, p_ct: round3(calibrate(raw)), p_raw: round3(raw), tension: round3(tension) });
      }
      const a = roundAnswers[j++];
      return {
        n: r.n,
        moments: ms,
        swing_tick: swingTick(ms),
        highlight: round3(a?.highlight?.type === "score" ? a.highlight.score : 0),
        share: round3(a?.share?.type === "noul" ? a.share.noul : 0),
        headline: a?.headline?.type === "choice" ? checkedHeadline(a.headline.choice, r.facts) : null,
        facts: { clutch: r.facts.clutch, multikill: r.facts.multikill, comeback: r.facts.comeback, how_won: r.facts.how_won },
      };
    });
    const fixtureDir = join(dir, g.fixture);
    mkdirSync(fixtureDir);
    writeFileSync(join(fixtureDir, "match.json"), JSON.stringify(g.match));
    writeFileSync(join(fixtureDir, "pulse.json"), JSON.stringify(pulseFile([...models][0] ?? "jev", "eval", out)));
  }
  const run = spawnSync(process.execPath, [join("..", "schema", "validate.mjs"), "--strict", dir], { encoding: "utf8" });
  if (run.status !== 0) console.log(run.stdout, run.stderr);
  return run.status === 0;
}

function brier(xs: { p: number; y: number }[]): number {
  return xs.reduce((s, { p, y }) => s + (p - y) ** 2, 0) / xs.length;
}

// The chance a random CT-won moment scores above a random T-won one.
function auc(xs: { p: number; y: number }[]): number {
  const pos = xs.filter((x) => x.y === 1).map((x) => x.p);
  const neg = xs.filter((x) => x.y === 0).map((x) => x.p);
  let wins = 0;
  for (const p of pos) for (const n of neg) wins += p > n ? 1 : p === n ? 0.5 : 0;
  return wins / (pos.length * neg.length);
}

// Expected calibration error over 5 equal-width bins.
function ece(xs: { p: number; y: number }[]): number {
  const bins = Array.from({ length: 5 }, () => ({ n: 0, p: 0, y: 0 }));
  for (const { p, y } of xs) {
    const b = bins[Math.min(4, Math.floor(p * 5))]!;
    b.n++;
    b.p += p;
    b.y += y;
  }
  return bins.reduce((s, b) => (b.n ? s + Math.abs(b.p / b.n - b.y / b.n) * b.n : s), 0) / xs.length;
}

function round3(v: number): number {
  return Math.round(v * 1000) / 1000;
}
