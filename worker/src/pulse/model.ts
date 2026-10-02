// From Jev's answers, or the fallback model, to pulse.json: calibration, the
// facts code computes, the swing and the rounds worth watching. See
// .claude/ideas/jev/spec.md and schema/pulse.schema.json.

import type { Answers, Question } from "./jev";
import { type RoundView, type Side, STATE_VERSION } from "./state";

export const PULSE_VERSION = 1;

// Raw Jev is underconfident (at 65% the CTs won 85%), so p_ct =
// sigmoid(logit(p_raw) / T). Fitted on 1,593 eval moments; every
// leave-one-match-out fold gave 0.52-0.54.
export const TEMPERATURE = 0.54;

export const FALLBACK_MODEL = "fallback-logit-v1";

// The fallback's weights, fitted on the same 1,593 moments (held-out Brier
// 0.138 against recalibrated Jev's 0.134): intercept, CT minus T alive,
// planted, CT alive, T alive, planted x CT alive, share of the round clock
// left.
const FALLBACK_WEIGHTS = [0.0227, 0.9572, -0.5965, 0.4769, -0.4803, 0.1056, 0.2663];

export const MOMENT_QUESTIONS: Record<string, Question> = {
  ct_wins: {
    type: "noul",
    instructions: "Will the CT side win this round from this moment?",
    criteria: {
      true: "The CTs win: they eliminate every T, defuse the bomb, or run out the clock with no plant",
      false: "The Ts win: they eliminate every CT or the bomb explodes",
    },
  },
  tension: {
    type: "score",
    instructions: "How tense is this moment for a spectator?",
    criteria: ["Decided: one side clearly has it", "Leaning one way", "Open: either side could win", "Knife-edge: one duel decides it"],
  },
};

const HOW_WON = {
  t_post_plant: "The Ts won after planting the bomb: it exploded, or they killed the remaining CTs after the plant",
  ct_retake: "The CTs won after the bomb was planted: they defused it or killed the remaining Ts after the plant",
  t_before_plant: "The Ts killed every CT before any bomb plant",
  ct_hold: "The CTs killed every T before any bomb plant",
  ct_time: "The CTs won because time ran out with no bomb planted",
};

const HEADLINES = {
  ace: "One player killed all five opponents",
  clutch_big: "The last player alive on the winning side beat two or more opponents",
  clutch_1v1: "The round came down to a 1v1",
  flawless: "The winning side lost nobody",
  comeback: "The winning side was down two or more players and turned it around",
  retake: "The CTs retook the site after the plant",
  post_plant: "The Ts held the planted bomb until the end",
  eco_upset: "A team on an eco or semi-eco beat a better-equipped team",
  awp_show: "An AWP player decided the round",
  trade_war: "Even trade-for-trade fighting, decided late",
  fast_win: "One side won quickly with little resistance",
};

export const ROUND_QUESTIONS: Record<string, Question> = {
  highlight: {
    type: "score",
    instructions: "How worth rewatching is this round for someone who plays CS2?",
    criteria: [
      "Routine: one side rolled over the other with nothing notable",
      "Some good plays, but a normal round",
      "A notable play: a multi-kill, a clean retake, or a tense finish",
      "A standout: a clutch, a comeback, or a player taking over the round",
      "Instant highlight: an ace, a 1v3 or bigger clutch, or a wild comeback",
    ],
  },
  share: { type: "noul", instructions: "Would a player in this round want to send a clip of it to friends?" },
  headline: { type: "choice", instructions: "Which headline fits this round best?", criteria: HEADLINES },
  how_won: {
    type: "choice",
    instructions: "How did the winning side win this round? Use the result and the timeline.",
    criteria: HOW_WON,
  },
};

const sigmoid = (z: number) => 1 / (1 + Math.exp(-z));
const logit = (p: number, e = 1e-3) => {
  const q = Math.min(Math.max(p, e), 1 - e);
  return Math.log(q / (1 - q));
};

export function calibrate(pRaw: number, temperature = TEMPERATURE): number {
  return sigmoid(logit(pRaw) / temperature);
}

// The fallback's P(CT wins). Without the bomb timeline (state_version 1)
// nothing is ever known to be planted.
export function fallbackPCt(alive: Record<Side, number>, sec: number, planted = false): number {
  const p = planted ? 1 : 0;
  const left = Math.max(0, 115 - sec) / 115;
  const x = [1, alive.CT - alive.T, p, alive.CT, alive.T, p * alive.CT, left];
  return sigmoid(x.reduce((sum, v, i) => sum + v * FALLBACK_WEIGHTS[i]!, 0));
}

export interface RoundFacts {
  clutch: string | null;
  multikill: number;
  comeback: boolean;
  how_won: "t_post_plant" | "ct_retake" | "t_before_plant" | "ct_hold" | "ct_time" | "surrender";
}

// Computed in code, never asked: Jev miscounts (comeback was right 31% of the
// time). Until the bomb timeline arrives, a plant is only known from the
// round's end reason, so an elimination after a plant reads as before it.
export function roundFacts(view: RoundView): RoundFacts & { flawless: boolean; oneVOne: boolean } {
  const winner = view.winner;
  const loser: Side = winner === "CT" ? "T" : "CT";
  const reason = view.round.reason;
  const kills = view.events.filter((e) => e.kill && !e.afterEnd);
  let clutchN = 0;
  let comeback = false;
  let oneVOne = false;
  for (const e of kills) {
    if (e.alive[winner] === 1 && e.alive[loser] >= 1) clutchN = Math.max(clutchN, e.alive[loser]);
    if (e.alive[loser] - e.alive[winner] >= 2) comeback = true;
    if (e.alive.CT === 1 && e.alive.T === 1) oneVOne = true;
  }
  const byPlayer = new Map<number, number>();
  for (const e of kills) {
    const k = e.kill!;
    if (k.attacker !== null && k.aside !== k.vside) byPlayer.set(k.attacker, (byPlayer.get(k.attacker) ?? 0) + 1);
  }
  const planted = reason === "bomb_exploded" || reason === "bomb_defused";
  const how_won: RoundFacts["how_won"] =
    reason === "surrender"
      ? "surrender"
      : reason === "time_expired"
        ? "ct_time"
        : planted
          ? winner === "T" ? "t_post_plant" : "ct_retake"
          : winner === "T" ? "t_before_plant" : "ct_hold";
  return {
    clutch: clutchN > 0 ? `1v${Math.min(clutchN, 5)}` : null,
    multikill: Math.min(5, Math.max(0, ...byPlayer.values())),
    comeback,
    how_won,
    flawless: !kills.some((e) => e.kill!.vside === winner),
    oneVOne,
  };
}

// Jev's headline, kept only when a check in code agrees. Templates with no
// check (awp_show, trade_war, fast_win) are kept; eco_upset needs economy,
// which state_version 1 lacks, so it is dropped.
export function checkedHeadline(choice: string, facts: ReturnType<typeof roundFacts>): string | null {
  const checks: Record<string, boolean> = {
    ace: facts.multikill >= 5,
    clutch_big: facts.clutch !== null && facts.clutch !== "1v1",
    clutch_1v1: facts.oneVOne,
    flawless: facts.flawless,
    comeback: facts.comeback,
    retake: facts.how_won === "ct_retake",
    post_plant: facts.how_won === "t_post_plant",
    eco_upset: false,
    awp_show: true,
    trade_war: true,
    fast_win: true,
  };
  return checks[choice] ? choice : null;
}

export interface Moment {
  tick: number;
  p_ct: number;
  p_raw: number;
  tension: number;
}

export interface PulseRoundOut {
  n: number;
  moments: Moment[];
  swing_tick: number | null;
  highlight: number;
  share: number;
  headline: string | null;
  facts: RoundFacts;
}

export interface PulseFile {
  pulse_version: number;
  model: string;
  state_version: number;
  parse_id: string;
  calibration: { temperature: number };
  rounds: PulseRoundOut[];
  worth_watching: number[];
}

// The tick of the moment whose p_ct moved most from the one before it.
export function swingTick(moments: Moment[]): number | null {
  let best: number | null = null;
  let size = -1;
  for (let i = 1; i < moments.length; i++) {
    const d = Math.abs(moments[i]!.p_ct - moments[i - 1]!.p_ct);
    if (d > size) [best, size] = [moments[i]!.tick, d];
  }
  return best;
}

const r3 = (v: number) => Math.round(v * 1000) / 1000;

// One round of pulse.json from Jev's answers: one set per moment, then the
// round call's.
export function jevRound(view: RoundView, momentAnswers: Answers[], roundAnswers: Answers): PulseRoundOut {
  const ticks = view.cuts.map((i) => view.events[i]!.tick);
  const moments = momentAnswers.map((a, i) => {
    const pRaw = noul(a.ct_wins);
    return { tick: ticks[i]!, p_ct: r3(calibrate(pRaw)), p_raw: r3(pRaw), tension: r3(score(a.tension)) };
  });
  const facts = roundFacts(view);
  const headline = roundAnswers.headline?.type === "choice" ? checkedHeadline(roundAnswers.headline.choice, facts) : null;
  return {
    n: view.n,
    moments,
    swing_tick: swingTick(moments),
    highlight: r3(score(roundAnswers.highlight)),
    share: r3(noul(roundAnswers.share)),
    headline,
    facts: { clutch: facts.clutch, multikill: facts.multikill, comeback: facts.comeback, how_won: facts.how_won },
  };
}

// One round from the fallback model alone. It has no highlight, share or
// headline; tension stands in as closeness, 3 at a coin flip and 0 when
// decided, which is what Jev's tension tracks (r 0.82).
export function fallbackRound(view: RoundView): PulseRoundOut {
  const moments = view.cuts.map((i) => {
    const e = view.events[i]!;
    const p = fallbackPCt(e.alive, e.sec);
    return { tick: e.tick, p_ct: r3(p), p_raw: r3(p), tension: r3(3 * (1 - Math.abs(2 * p - 1))) };
  });
  const facts = roundFacts(view);
  return {
    n: view.n,
    moments,
    swing_tick: swingTick(moments),
    highlight: 0,
    share: 0,
    headline: null,
    facts: { clutch: facts.clutch, multikill: facts.multikill, comeback: facts.comeback, how_won: facts.how_won },
  };
}

export function pulseFile(model: string, parseId: string, rounds: PulseRoundOut[]): PulseFile {
  const worth =
    model === FALLBACK_MODEL
      ? []
      : [...rounds].sort((a, b) => b.highlight - a.highlight || a.n - b.n).slice(0, 3).map((r) => r.n);
  return {
    pulse_version: PULSE_VERSION,
    model,
    state_version: STATE_VERSION,
    parse_id: parseId,
    calibration: { temperature: model === FALLBACK_MODEL ? 1 : TEMPERATURE },
    rounds,
    worth_watching: worth,
  };
}

// How well the line called the rounds, scored against the real winners as
// soon as they're known: the self-grading row.
export function grade(file: PulseFile, winners: Map<number, Side>): { moments: number; brier: number; logloss: number } {
  let n = 0;
  let brier = 0;
  let logloss = 0;
  for (const r of file.rounds) {
    const y = winners.get(r.n) === "CT" ? 1 : 0;
    for (const m of r.moments) {
      const p = Math.min(Math.max(m.p_ct, 1e-6), 1 - 1e-6);
      brier += (p - y) ** 2;
      logloss -= y * Math.log(p) + (1 - y) * Math.log(1 - p);
      n++;
    }
  }
  return { moments: n, brier: n ? r3(brier / n) : 0, logloss: n ? r3(logloss / n) : 0 };
}

function noul(a: Answers[string] | undefined): number {
  if (a?.type !== "noul") throw new Error("expected a noul answer");
  return a.noul;
}

function score(a: Answers[string] | undefined): number {
  if (a?.type !== "score") throw new Error("expected a score answer");
  return a.score;
}
