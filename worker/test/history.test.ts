import { createMessageBatch } from "cloudflare:test";
import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { handleRequest, type ParseJob } from "../src/api";
import { handleParseBatch } from "../src/consumer";
import type { MatchFile } from "../src/match-file";
import type { ParseResult } from "../src/parser";
import { events, insertMatch, request, testEnv, TOKEN } from "./helpers";

const ID = "0123456789abc";

const FILE: MatchFile = {
  schema_version: 1,
  parser_version: "0.1.0",
  match: { map: "de_mirage", tick_rate: 64, demo_sha256: "ab".repeat(32) },
  players: [],
  rounds: [{ n: 1, winner: "A", reason: "elimination", side_a: "CT" }],
  kills: [],
  scoreboard: [],
};

async function consume(result: ParseResult, attempts = 1, body: ParseJob = { match_id: ID }) {
  const batch = createMessageBatch<ParseJob>("demoview-dev-parse", [
    { id: `msg-${attempts}`, timestamp: new Date(), attempts, body },
  ]);
  let parseId = "";
  await handleParseBatch(batch, env, {
    parse: async (req) => {
      parseId = req.parse_id;
      return result;
    },
  });
  return parseId;
}

describe("match history", () => {
  it("records each status change and attempt", async () => {
    await insertMatch(ID, "queued");
    const first = await consume({ kind: "transient", status: 503, error: "parser returned 503" });
    const second = await consume({ kind: "ok", status: 200, body: FILE }, 2);

    const build = "test-build";
    expect(await events(ID)).toEqual([
      { kind: "status", msg: "parsing", attempt: 1, build: null, parse_id: null },
      { kind: "attempt", msg: expect.stringMatching(/^failed in \d+\.\d s: parser returned 503$/), attempt: 1, build, parse_id: first },
      { kind: "status", msg: "queued: parser returned 503", attempt: 1, build: null, parse_id: null },
      { kind: "status", msg: "parsing", attempt: 2, build: null, parse_id: null },
      { kind: "attempt", msg: expect.stringMatching(/^parsed in \d+\.\d s$/), attempt: 2, build, parse_id: second },
      { kind: "status", msg: "ready", attempt: 2, build: null, parse_id: second },
    ]);
  });

  it("records a reparse's failure without a status change", async () => {
    await insertMatch(ID, "ready");
    await consume({ kind: "rejected", status: 422, code: "bad_demo", error: "truncated" }, 1, {
      match_id: ID,
      reparse: true,
    });
    expect(await events(ID)).toEqual([
      {
        kind: "attempt",
        msg: expect.stringMatching(/^reparse rejected in \d+\.\d s: bad_demo: truncated$/),
        attempt: 1,
        build: "test-build",
        parse_id: expect.any(String),
      },
    ]);
  });

  it("counts each parse call's container time, plus the idle tail", async () => {
    await insertMatch(ID, "queued");
    await consume({ kind: "ok", status: 200, body: FILE });
    const ms = await env.DB.prepare("SELECT ms FROM container_usage").first<number>("ms");
    expect(ms).toBeGreaterThanOrEqual(30_000);
    expect(ms).toBeLessThan(31_000);
  });

  it("is served by GET /api/matches/:id/events, oldest first", async () => {
    const created = await handleRequest(request("POST", "/api/matches", { token: TOKEN }), env);
    const { id } = await created.json<{ id: string }>();
    const res = await handleRequest(request("GET", `/api/matches/${id}/events`), env);
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toEqual({
      events: [
        {
          seq: 1,
          ts: expect.any(Number),
          kind: "status",
          msg: "uploaded",
          attempt: null,
          build: null,
          parse_id: null,
        },
      ],
    });
    expect((await handleRequest(request("GET", "/api/matches/0000000000000/events"), env)).status).toBe(404);
  });

  it("goes with a duplicate upload's deleted match", async () => {
    await insertMatch(ID, "ready", { demo_sha256: "2eb3b002642ecce9fd1a8b767128cdddab882ae26245abec2fff9212ad3b3d15" });
    const created = await handleRequest(request("POST", "/api/matches", { token: TOKEN }), env);
    const { id } = await created.json<{ id: string }>();
    await env.BUCKET.put(`demos/${id}.dem`, "PBDEMS2\0");
    const { env: e } = testEnv();
    await handleRequest(request("POST", `/api/matches/${id}/uploaded`, { token: TOKEN }), e);
    expect(await events(id)).toEqual([]);
  });
});
