import { createMessageBatch, getQueueResult, createExecutionContext } from "cloudflare:test";
import { env } from "cloudflare:workers";
import { describe, expect, it, vi } from "vitest";
import matchJson from "../../schema/samples/match.json";
import round01 from "../../schema/samples/rounds/01.json";
import round02 from "../../schema/samples/rounds/02.json";
import { handleRequest } from "../src/api";
import { classify, jevClient, type Answers, Ledger, Pacer } from "../src/pulse/jev";
import {
  calibrate,
  checkedHeadline,
  fallbackPCt,
  FALLBACK_MODEL,
  grade,
  pulseFile,
  roundFacts,
  swingTick,
  TEMPERATURE,
} from "../src/pulse/model";
import { handlePulseBatch, type JevFactory, type PulseJob } from "../src/pulse/consumer";
import { hpWord, type PulseMatch, type PulseRound, RoundView } from "../src/pulse/state";
import { events, insertMatch, matchRow, request, TOKEN } from "./helpers";

const match = matchJson as unknown as PulseMatch;
const ID = "0123456789abc";
const PARSE = "01k6gq3m1abcdefg";

// A fake Workers AI binding that answers every question the same way.
function fakeAi(answer: (questions: string[]) => Answers, fail?: () => Error | undefined) {
  const run = vi.fn(async (_model: string, input: { questions: Record<string, unknown> }) => {
    const err = fail?.();
    if (err) throw err;
    return { result: { answers: answer(Object.keys(input.questions)), model: "jev-test", usage: { input_tokens: 800 } } };
  });
  return { ai: { run } as unknown as Ai, run };
}

const steady = (questions: string[]): Answers =>
  Object.fromEntries(
    questions.map((q) => [
      q,
      q === "ct_wins" || q === "share"
        ? { type: "noul", noul: 0.6 }
        : q === "headline" || q === "how_won"
          ? { type: "choice", choice: q === "headline" ? "flawless" : "ct_hold", probabilities: {}, confidence: 1 }
          : { type: "score", score: 2, probabilities: {}, confidence: 1 },
    ]),
  ) as Answers;

const quick = () => new Pacer(4, 0);

describe("RoundView states", () => {
  const view = new RoundView(match, 1, round01 as unknown as PulseRound);

  it("reads freeze end, then each kill tick before the round ended", () => {
    expect(view.events[0]?.type).toBe("freeze_end");
    expect(view.cuts[0]).toBe(0);
    const r = match.rounds[0]!;
    const ticks = new Set(match.kills.filter((k) => k.round === 1 && k.tick > r.freeze_end_tick && k.tick <= r.end_tick).map((k) => k.tick));
    expect(view.cuts.length).toBe(1 + ticks.size);
  });

  it("makes one moment per tick, from freeze end on", () => {
    const m: PulseMatch = {
      match: { map: "de_test", tick_rate: 64 },
      players: [0, 1, 2, 3].map((idx) => ({ idx, team: idx < 2 ? ("A" as const) : ("B" as const) })),
      rounds: [{ n: 1, start_tick: 0, freeze_end_tick: 100, end_tick: 900, winner: "A", reason: "elimination", side_a: "CT" }],
      kills: [
        { round: 1, tick: 50, attacker: 0, victim: 2, weapon: "ak47", headshot: false }, // during freeze time
        { round: 1, tick: 100, attacker: 1, victim: 3, weapon: "awp", headshot: false }, // at freeze end
        { round: 1, tick: 400, attacker: 2, victim: 0, weapon: "ak47", headshot: false },
        { round: 1, tick: 400, attacker: 3, victim: 1, weapon: "ak47", headshot: false }, // same tick
        { round: 1, tick: 950, attacker: 0, victim: 1, weapon: "ak47", headshot: false }, // after the end
      ],
    };
    const v = new RoundView(m, 1, null);
    expect(v.cuts.map((i) => v.events[i]!.tick)).toEqual([100, 400]);
    expect(v.events[v.cuts[1]!]!.alive).toEqual({ CT: 0, T: 0 });
  });

  it("describes a moment with the clock, the alive count and HP words", () => {
    const state = view.state({ cut: view.cuts[2] }) as Record<string, any>;
    expect(Object.keys(state)).toEqual(["map", "round", "score_before_round", "players", "timeline", "now"]);
    expect(state.round).toBe("1");
    expect(state.score_before_round).toBe("CT 0 - T 0");
    expect(state.timeline).toHaveLength(2);
    expect(state.timeline[1]).toMatch(/^\d:\d\d p\d \((CT|T)\) kills p\d \((CT|T)\) \(.+\)\. Alive: \d CT v \d T\.$/);
    expect(state.now.time).toMatch(/^about \d+ seconds left in the round$/);
    expect(state.now.alive).toMatch(/^\d CT v \d T$/);
    expect(state.now.alive_players.CT.join(" ")).toMatch(/p\d (full HP|hurt|low HP|one shot from death)/);
  });

  it("starts at freeze end with no kills yet", () => {
    expect((view.state({ cut: 0 }) as Record<string, any>).timeline).toEqual(["Freeze time ends."]);
  });

  it("describes the whole round with line ids and the result", () => {
    const state = view.state({ outcome: true, ids: true }) as Record<string, any>;
    expect(state.round).toBe(`1 of ${match.rounds.length}`);
    expect(state.timeline[0]).toMatch(/^L00 /);
    expect(state.result).toMatch(/^(CT|T) win by /);
  });

  it("never names a player", () => {
    const text = JSON.stringify([view.state({ outcome: true }), ...view.cuts.map((cut) => view.state({ cut }))]);
    for (const p of (matchJson as { players: { name: string; steam_id: string }[] }).players) {
      expect(text).not.toContain(p.name);
      expect(text).not.toContain(p.steam_id);
    }
  });

  it("buckets HP into words", () => {
    expect([100, 90, 89, 50, 49, 20, 19, 1].map(hpWord)).toEqual([
      "full HP",
      "full HP",
      "hurt",
      "hurt",
      "low HP",
      "low HP",
      "one shot from death",
      "one shot from death",
    ]);
  });
});

describe("model", () => {
  it("sharpens Jev's underconfident odds", () => {
    expect(calibrate(0.5)).toBeCloseTo(0.5);
    expect(calibrate(0.65)).toBeGreaterThan(0.75);
    expect(calibrate(0.35)).toBeLessThan(0.25);
    expect(calibrate(0.65, 1)).toBeCloseTo(0.65);
    expect(TEMPERATURE).toBe(0.54);
  });

  it("draws a sensible fallback line", () => {
    const even = fallbackPCt({ CT: 5, T: 5 }, 0);
    expect(even).toBeGreaterThan(0.4);
    expect(even).toBeLessThan(0.65);
    expect(fallbackPCt({ CT: 1, T: 4 }, 30)).toBeLessThan(0.1);
    expect(fallbackPCt({ CT: 4, T: 1 }, 30)).toBeGreaterThan(0.9);
  });

  it("computes facts in code", () => {
    const facts = roundFacts(new RoundView(match, 1, round01 as unknown as PulseRound));
    expect(["ct_hold", "t_before_plant", "ct_retake", "t_post_plant", "ct_time"]).toContain(facts.how_won);
    expect(facts.multikill).toBeGreaterThanOrEqual(1);
    expect(facts.clutch === null || /^1v[1-5]$/.test(facts.clutch)).toBe(true);
  });

  it("keeps a headline only when code's check agrees", () => {
    const facts = { clutch: "1v2", multikill: 2, comeback: false, how_won: "ct_hold" as const, flawless: false, oneVOne: false };
    expect(checkedHeadline("clutch_big", facts)).toBe("clutch_big");
    expect(checkedHeadline("ace", facts)).toBeNull();
    expect(checkedHeadline("retake", facts)).toBeNull();
    expect(checkedHeadline("eco_upset", facts)).toBeNull();
    expect(checkedHeadline("awp_show", facts)).toBe("awp_show");
  });

  it("finds the swing and the rounds worth watching", () => {
    const m = (tick: number, p_ct: number) => ({ tick, p_ct, p_raw: p_ct, tension: 1 });
    expect(swingTick([m(1, 0.5), m(2, 0.4), m(3, 0.9), m(4, 0.95)])).toBe(3);
    expect(swingTick([m(1, 0.5)])).toBeNull();
    const round = (n: number, highlight: number) => ({ n, moments: [m(1, 0.5)], swing_tick: null, highlight, share: 0, headline: null, facts: { clutch: null, multikill: 0, comeback: false, how_won: "ct_hold" as const } });
    expect(pulseFile("jev-1", PARSE, [round(1, 1), round(2, 3.5), round(3, 2), round(4, 3.5)]).worth_watching).toEqual([2, 4, 3]);
    expect(pulseFile(FALLBACK_MODEL, PARSE, [round(1, 1)])).toMatchObject({ worth_watching: [], calibration: { temperature: 1 } });
  });

  it("grades the line against the winners", () => {
    const file = pulseFile("jev-1", PARSE, [
      { n: 1, moments: [{ tick: 1, p_ct: 0.9, p_raw: 0.8, tension: 1 }], swing_tick: null, highlight: 0, share: 0, headline: null, facts: { clutch: null, multikill: 0, comeback: false, how_won: "ct_hold" } },
    ]);
    expect(grade(file, new Map([[1, "CT"]]))).toEqual({ moments: 1, brier: 0.01, logloss: 0.105 });
  });
});

describe("jevClient", () => {
  it("unwraps the binding's answers and records the call", async () => {
    const { ai } = fakeAi(steady);
    const ledger = new Ledger();
    const out = await jevClient(ai, "default", ledger, quick()).ask({}, { ct_wins: { type: "noul", instructions: "?" } });
    expect(out).toEqual({ answers: { ct_wins: { type: "noul", noul: 0.6 } }, model: "jev-test" });
    expect(ledger).toMatchObject({ calls: 1, inTokens: 800 });
  });

  it("stops at once, for good, when credits run out", async () => {
    const { ai, run } = fakeAi(steady, () => new Error("AiError: 2021: Insufficient balance"));
    const jev = jevClient(ai, "default", new Ledger(), quick());
    await expect(jev.ask({}, {})).rejects.toMatchObject({ kind: "credits" });
    await expect(jev.ask({}, {})).rejects.toMatchObject({ kind: "credits" });
    expect(run).toHaveBeenCalledTimes(1);
  });

  it("classifies gateway errors", () => {
    expect(classify(new Error("429 code 2018: rate limited")).kind).toBe("rate_limit");
    expect(classify(new Error("2021 Insufficient balance")).kind).toBe("credits");
    expect(classify(new Error("boom")).kind).toBe("other");
  });
});

describe("pulse consumer", () => {
  const putParse = async () => {
    await insertMatch(ID, "ready", { live_parse_id: PARSE });
    const prefix = `parsed/${ID}/${PARSE}/`;
    await env.BUCKET.put(`${prefix}match.json`, JSON.stringify(matchJson));
    await env.BUCKET.put(`${prefix}rounds/01.json`, JSON.stringify(round01));
    await env.BUCKET.put(`${prefix}rounds/02.json`, JSON.stringify(round02));
  };

  async function run(makeJev: JevFactory, body: PulseJob = { match_id: ID, parse_id: PARSE }, attempts = 1) {
    const batch = createMessageBatch<PulseJob>("demoview-dev-pulse", [{ id: "m", timestamp: new Date(), attempts, body }]);
    await handlePulseBatch(batch, env, makeJev);
    const r = await getQueueResult(batch, createExecutionContext());
    return { acked: r.explicitAcks.includes("m"), retried: r.retryMessages.length > 0 };
  }

  const pulse = async () => (await env.BUCKET.get(`parsed/${ID}/${PARSE}/pulse.json`))?.json<Record<string, any>>();

  it("writes pulse.json from Jev, with a quality row, history and usage", async () => {
    await putParse();
    const { ai, run: calls } = fakeAi(steady);
    expect(await run((ledger) => jevClient(ai, "default", ledger, quick()))).toEqual({ acked: true, retried: false });

    const file = (await pulse())!;
    expect(file).toMatchObject({ pulse_version: 1, model: "jev-test", state_version: 1, parse_id: PARSE, calibration: { temperature: 0.54 } });
    const rounds = match.rounds.filter((r) => r.reason !== "surrender");
    expect(file.rounds).toHaveLength(rounds.length);
    expect(file.rounds[0].moments[0]).toEqual({ tick: match.rounds[0]!.freeze_end_tick, p_ct: 0.679, p_raw: 0.6, tension: 2 });
    expect(file.worth_watching).toHaveLength(3);
    expect(calls).toHaveBeenCalledTimes(file.rounds.reduce((s: number, r: any) => s + r.moments.length + 1, 0));

    const quality = await env.DB.prepare("SELECT * FROM pulse_quality").first<Record<string, any>>();
    expect(quality).toMatchObject({ match_id: ID, parse_id: PARSE, model: "jev-test", rounds: rounds.length });
    expect((await events(ID)).at(-1)).toMatchObject({ kind: "pulse", parse_id: PARSE });
    expect(await env.DB.prepare("SELECT calls FROM ai_usage").first<number>("calls")).toBe(calls.mock.calls.length);
  });

  it("falls back to the logit when credits run out, without retrying", async () => {
    await putParse();
    const { ai } = fakeAi(steady, () => new Error("2021: Insufficient balance"));
    expect(await run((ledger) => jevClient(ai, "default", ledger, quick()))).toEqual({ acked: true, retried: false });
    expect(await pulse()).toMatchObject({ model: FALLBACK_MODEL, worth_watching: [] });
    expect(await env.DB.prepare("SELECT count(*) AS n FROM pulse_quality").first<number>("n")).toBe(0);
  });

  it("retries Jev on other failures, then falls back on the last attempt", async () => {
    await putParse();
    const { ai } = fakeAi(steady, () => new Error("boom"));
    const jev: JevFactory = (ledger) => jevClient(ai, "default", ledger, quick());
    expect(await run(jev, undefined, 1)).toEqual({ acked: false, retried: true });
    expect(await pulse()).toBeUndefined();
    expect(await run(jev, undefined, 3)).toEqual({ acked: true, retried: false });
    expect(await pulse()).toMatchObject({ model: FALLBACK_MODEL });
  });

  it("skips a parse that isn't live, and one that already has a Pulse", async () => {
    await putParse();
    const { ai, run: calls } = fakeAi(steady);
    const jev: JevFactory = (ledger) => jevClient(ai, "default", ledger, quick());
    expect(await run(jev, { match_id: ID, parse_id: "01k6gq3m1zzzzzzz" })).toEqual({ acked: true, retried: false });
    await env.BUCKET.put(`parsed/${ID}/${PARSE}/pulse.json`, "{}");
    expect(await run(jev)).toEqual({ acked: true, retried: false });
    expect(calls).not.toHaveBeenCalled();
  });
});

describe("pulse routes", () => {
  it("serve a parse's pulse.json, or 404 before it exists", async () => {
    await insertMatch(ID, "ready", { live_parse_id: PARSE });
    const get = () => handleRequest(request("GET", `/api/matches/${ID}/pulse.json?v=${PARSE}`), env);
    expect((await get()).status).toBe(404);
    await env.BUCKET.put(`parsed/${ID}/${PARSE}/pulse.json`, '{"pulse_version":1}');
    const res = await get();
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("public, max-age=300");
  });

  it("queue a Pulse for the live parse, behind the token and the switch", async () => {
    await insertMatch(ID, "ready", { live_parse_id: PARSE });
    const sent: unknown[] = [];
    const on = { ...env, PULSE_ENABLED: "true", PULSE_QUEUE: { send: async (m: unknown) => void sent.push(m) } as unknown as Queue };
    const post = (e: Env, token = TOKEN) => handleRequest(request("POST", `/api/matches/${ID}/pulse`, { token }), e);
    expect((await post(on, "")).status).toBe(401);
    expect((await post(env)).status).toBe(409);
    expect((await post(on)).status).toBe(202);
    expect(sent).toEqual([{ match_id: ID, parse_id: PARSE }]);
    expect((await matchRow(ID))?.status).toBe("ready");
  });
});
