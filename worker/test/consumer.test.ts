import { createExecutionContext, createMessageBatch, getQueueResult } from "cloudflare:test";
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ParseJob } from "../src/api";
import { handleDeadLetters, handleParseBatch } from "../src/consumer";
import worker from "../src/index";
import type { MatchFile } from "../src/match-file";
import type { Parser, ParseResult } from "../src/parser";
import { insertMatch, matchRow } from "./helpers";

const ID = "0123456789abc";
const QUEUE = "demoview-dev-parse";
const DLQ = "demoview-dev-parse-dlq";

const FILE: MatchFile = {
  schema_version: 1,
  parser_version: "0.1.0",
  match: { map: "de_mirage", tick_rate: 64, demo_sha256: "ab".repeat(32) },
  players: [
    { idx: 0, steam_id: "76561198000000000", name: "alice", team: "A" },
    { idx: 1, steam_id: "76561198000000001", name: "bob", team: "B" },
  ],
  rounds: [
    { n: 1, winner: "A", reason: "bomb_defused", side_a: "CT" },
    { n: 2, winner: "B", reason: "t_win", side_a: "CT" },
    { n: 3, winner: "A", reason: "ct_win", side_a: "CT" },
  ],
  kills: [
    { round: 1, tick: 100, attacker: 0, victim: 1, weapon: "ak47", headshot: true },
    { round: 2, tick: 200, attacker: null, victim: 0, weapon: "world", headshot: false },
  ],
  scoreboard: [
    { player: 0, kills: 1, deaths: 1, assists: 0, adr: 100, hs_pct: 1 },
    { player: 1, kills: 0, deaths: 1, assists: 0, adr: 25.5, hs_pct: 0 },
  ],
};

function stubParser(result: ParseResult | (() => Promise<ParseResult>)): Parser & {
  parse: ReturnType<typeof vi.fn>;
} {
  return { parse: vi.fn(typeof result === "function" ? result : async () => result) };
}

function batch(queue: string, body: unknown, attempts = 1) {
  return createMessageBatch<ParseJob>(queue, [
    { id: "msg-1", timestamp: new Date(), attempts, body: body as ParseJob },
  ]);
}

// Runs one message through `handle` and reports whether it was acked or retried.
async function run(queue: string, body: unknown, handle: (b: MessageBatch<ParseJob>) => Promise<void>) {
  const b = batch(queue, body);
  const ctx = createExecutionContext();
  await handle(b);
  const result = await getQueueResult(b, ctx);
  return {
    acked: result.explicitAcks.includes("msg-1"),
    retried: result.retryMessages.some((m) => m.msgId === "msg-1"),
  };
}

const parse = (parser: Parser, body: unknown = { match_id: ID }) =>
  run(QUEUE, body, (b) => handleParseBatch(b, env, parser));

const rows = (sql: string) => env.DB.prepare(sql).all().then((r) => r.results);

afterEach(() => {
  vi.restoreAllMocks();
});

describe("parse queue consumer", () => {
  it("writes the parse result to D1 and marks the match ready", async () => {
    await insertMatch(ID, "queued");
    const parser = stubParser({ kind: "ok", status: 200, body: FILE });

    expect(await parse(parser)).toEqual({ acked: true, retried: false });
    expect(parser.parse).toHaveBeenCalledWith(
      { match_id: ID, demo_key: `demos/${ID}.dem` },
      { attempt: 1, requestId: "msg-1" },
    );

    expect(await matchRow(ID)).toMatchObject({
      status: "ready",
      error: null,
      map: "de_mirage",
      score_a: 2,
      score_b: 1,
      demo_sha256: "ab".repeat(32),
      parser_version: "0.1.0",
      schema_version: 1,
    });
    expect((await matchRow(ID))?.parsed_at).toEqual(expect.any(Number));
    expect(await rows("SELECT * FROM players ORDER BY idx")).toEqual([
      {
        match_id: ID, idx: 0, steam_id: "76561198000000000", name: "alice", team: "A",
        kills: 1, deaths: 1, assists: 0, adr: 100, hs_pct: 1,
      },
      {
        match_id: ID, idx: 1, steam_id: "76561198000000001", name: "bob", team: "B",
        kills: 0, deaths: 1, assists: 0, adr: 25.5, hs_pct: 0,
      },
    ]);
    expect(await rows("SELECT * FROM rounds ORDER BY n")).toEqual([
      { match_id: ID, n: 1, winner: "A", reason: "bomb_defused", side_a: "CT" },
      { match_id: ID, n: 2, winner: "B", reason: "t_win", side_a: "CT" },
      { match_id: ID, n: 3, winner: "A", reason: "ct_win", side_a: "CT" },
    ]);
    expect(await rows("SELECT * FROM kills ORDER BY tick")).toEqual([
      { match_id: ID, round: 1, tick: 100, attacker: 0, victim: 1, weapon: "ak47", headshot: 1 },
      { match_id: ID, round: 2, tick: 200, attacker: null, victim: 0, weapon: "world", headshot: 0 },
    ]);
  });

  it("replaces earlier rows when a match is parsed again", async () => {
    await insertMatch(ID, "queued");
    await parse(stubParser({ kind: "ok", status: 200, body: FILE }));
    await env.DB.prepare("UPDATE matches SET status = 'queued' WHERE id = ?").bind(ID).run();
    await parse(stubParser({ kind: "ok", status: 200, body: FILE }));

    expect(await rows("SELECT count(*) AS n FROM players")).toEqual([{ n: 2 }]);
    expect(await rows("SELECT count(*) AS n FROM rounds")).toEqual([{ n: 3 }]);
    expect(await rows("SELECT count(*) AS n FROM kills")).toEqual([{ n: 2 }]);
  });

  it("stores a large match in a handful of statements", async () => {
    await insertMatch(ID, "queued");
    const kills = Array.from({ length: 400 }, (_, i) => ({ ...FILE.kills[0]!, tick: i }));
    const spy = vi.spyOn(env.DB, "batch");
    await parse(stubParser({ kind: "ok", status: 200, body: { ...FILE, kills } }));

    expect(await rows("SELECT count(*) AS n FROM kills")).toEqual([{ n: 400 }]);
    expect(spy.mock.calls[0]?.[0].length).toBeLessThanOrEqual(10);
  });

  it("sets parsing while the parser runs", async () => {
    await insertMatch(ID, "queued");
    let statusDuringParse: string | undefined;
    await parse(
      stubParser(async () => {
        statusDuringParse = (await matchRow(ID))?.status;
        return { kind: "ok", status: 200, body: FILE };
      }),
    );
    expect(statusDuringParse).toBe("parsing");
  });

  it("fails the match without retrying on 422", async () => {
    await insertMatch(ID, "queued");
    const result = await parse(stubParser({ kind: "rejected", status: 422, code: "bad_demo", error: "not a CS2 demo" }));
    expect(result).toEqual({ acked: true, retried: false });
    expect(await matchRow(ID)).toMatchObject({ status: "failed", error: "bad_demo: not a CS2 demo" });
  });

  it("fails the match without retrying when match.json is malformed", async () => {
    await insertMatch(ID, "queued");
    const result = await parse(stubParser({ kind: "ok", status: 200, body: { ...FILE, schema_version: 2 } }));
    expect(result).toEqual({ acked: true, retried: false });
    expect(await matchRow(ID)).toMatchObject({
      status: "failed",
      error: "bad_output: match.json has schema_version 2, expected 1",
    });
  });

  it("requeues and retries on a transient failure, keeping the error", async () => {
    await insertMatch(ID, "queued");
    const result = await parse(stubParser({ kind: "transient", error: "parser returned 503" }));
    expect(result).toEqual({ acked: false, retried: true });
    expect(await matchRow(ID)).toMatchObject({ status: "queued", error: "parser returned 503" });
  });

  it("clears the previous attempt's error when a retry succeeds", async () => {
    await insertMatch(ID, "queued", { error: "parser returned 503" });
    await parse(stubParser({ kind: "ok", status: 200, body: FILE }));
    expect(await matchRow(ID)).toMatchObject({ status: "ready", error: null });
  });

  it("retries when the parser throws", async () => {
    await insertMatch(ID, "queued");
    const result = await parse(
      stubParser(async () => {
        throw new Error("boom");
      }),
    );
    expect(result).toEqual({ acked: false, retried: true });
  });

  it("picks up a match left in parsing by an interrupted attempt", async () => {
    await insertMatch(ID, "parsing");
    await parse(stubParser({ kind: "ok", status: 200, body: FILE }));
    expect((await matchRow(ID))?.status).toBe("ready");
  });

  it.each(["uploaded", "ready", "failed"])("acks and skips a %s match", async (status) => {
    await insertMatch(ID, status);
    const parser = stubParser({ kind: "ok", status: 200, body: FILE });
    expect(await parse(parser)).toEqual({ acked: true, retried: false });
    expect(parser.parse).not.toHaveBeenCalled();
    expect((await matchRow(ID))?.status).toBe(status);
  });

  it.each([{ match_id: ID }, { match_id: "../x" }, {}, null])(
    "acks a message for a missing or invalid match: %j",
    async (body) => {
      const parser = stubParser({ kind: "ok", status: 200, body: FILE });
      expect(await parse(parser, body)).toEqual({ acked: true, retried: false });
      expect(parser.parse).not.toHaveBeenCalled();
    },
  );
});

describe("dead letter consumer", () => {
  const deadLetter = (body: unknown = { match_id: ID }) =>
    run(DLQ, body, (b) => handleDeadLetters(b, env));

  it("fails the match with the last recorded error", async () => {
    await insertMatch(ID, "queued", { error: "parser returned 503" });
    expect(await deadLetter()).toEqual({ acked: true, retried: false });
    expect(await matchRow(ID)).toMatchObject({
      status: "failed",
      error: "parse_failed: failed after 3 attempts: parser returned 503",
    });
  });

  it("fails a match stuck in parsing with no recorded error", async () => {
    await insertMatch(ID, "parsing");
    await deadLetter();
    expect(await matchRow(ID)).toMatchObject({
      status: "failed",
      error: "parse_failed: failed after 3 attempts: unknown error",
    });
  });

  it("leaves a match that already finished alone", async () => {
    await insertMatch(ID, "ready");
    expect(await deadLetter()).toEqual({ acked: true, retried: false });
    expect(await matchRow(ID)).toMatchObject({ status: "ready", error: null });
  });

  it("acks messages for unknown matches", async () => {
    expect(await deadLetter()).toEqual({ acked: true, retried: false });
  });
});

describe("Worker queue handler", () => {
  async function deliver(queue: string, e: Env = env) {
    const b = batch(queue, { match_id: ID });
    const ctx = createExecutionContext();
    await worker.queue(b, e);
    return getQueueResult(b, ctx);
  }

  // Containers can't run in tests, so the parser binding is a fake whose
  // container answers every request with response.
  function fakeParser(response: Response) {
    const fetch = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => response);
    const binding = { idFromName: (name: string) => name, get: () => ({ fetch }) };
    return { env: { ...env, PARSER: binding as unknown as Env["PARSER"] }, fetch };
  }

  it("sends parse jobs to the parser container", async () => {
    await insertMatch(ID, "queued");
    const { env: e, fetch } = fakeParser(Response.json({ error: "truncated demo" }, { status: 422 }));

    await deliver(QUEUE, e);
    const [url, init] = fetch.mock.calls[0] ?? [];
    expect(String(url)).toBe("http://parser/parse");
    expect(new Headers(init?.headers).get("x-parse-secret")).toBe("test-parse-secret");
    expect(await matchRow(ID)).toMatchObject({ status: "failed", error: "bad_demo: truncated demo" });
  });

  it("routes the dead letter queue to the dead letter consumer", async () => {
    await insertMatch(ID, "queued", { error: "parser returned 503" });
    const fetchSpy = vi.spyOn(globalThis, "fetch");

    await deliver(DLQ);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect((await matchRow(ID))?.status).toBe("failed");
  });
});
