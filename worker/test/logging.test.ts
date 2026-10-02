import { createExecutionContext, createMessageBatch, getQueueResult } from "cloudflare:test";
import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { handleRequest, type ParseJob } from "../src/api";
import { handleDeadLetters, handleParseBatch } from "../src/consumer";
import type { Parser, ParseResult } from "../src/parser";
import { insertMatch, matchRow, request, testEnv, TOKEN } from "./helpers";

const ID = "0123456789abc";

type Level = "log" | "warn" | "error";
let lines: { level: Level; entry: Record<string, unknown> }[];

beforeEach(() => {
  lines = [];
  for (const level of ["log", "warn", "error"] as const) {
    vi.spyOn(console, level).mockImplementation((text: unknown) => {
      lines.push({ level, entry: JSON.parse(String(text)) });
    });
  }
});

afterEach(() => {
  vi.restoreAllMocks();
});

const events = (name: string) => lines.filter((l) => l.entry.event === name);

async function consume(result: ParseResult | Error, attempts = 1) {
  const parser: Parser = {
    parse: async () => {
      if (result instanceof Error) throw result;
      return result;
    },
  };
  const batch = createMessageBatch<ParseJob>("demoview-dev-parse", [
    { id: "msg-1", timestamp: new Date(), attempts, body: { match_id: ID } },
  ]);
  await handleParseBatch(batch, env, parser);
  return getQueueResult(batch, createExecutionContext());
}

describe("match_status", () => {
  it("carries the attempt, at warn while retrying and error once failed", async () => {
    await insertMatch(ID, "queued");
    await consume({ kind: "transient", status: 503, error: "parser returned 503" }, 2);

    expect(events("match_status")).toEqual([
      { level: "log", entry: { event: "match_status", match_id: ID, status: "parsing", attempt: 2 } },
      {
        level: "warn",
        entry: { event: "match_status", match_id: ID, status: "queued", attempt: 2, error: "parser returned 503" },
      },
    ]);

    await consume({ kind: "rejected", status: 422, code: "bad_demo", error: "bad magic" }, 3);
    expect(events("match_status").at(-1)).toEqual({
      level: "error",
      entry: { event: "match_status", match_id: ID, status: "failed", attempt: 3, error: "bad_demo: bad magic" },
    });
  });
});

describe("parse_call", () => {
  it("logs one line per attempt with the outcome", async () => {
    await insertMatch(ID, "queued");
    await consume({ kind: "transient", status: 503, error: "parser returned 503" }, 3);

    const [call] = events("parse_call");
    expect(call?.level).toBe("warn");
    expect(call?.entry).toMatchObject({
      match_id: ID,
      attempt: 3,
      max_attempts: 3,
      final: true,
      status: 503,
      result: "transient",
      error: "parser returned 503",
    });
    expect(call?.entry.duration_ms).toEqual(expect.any(Number));
  });
});

describe("consumer_error", () => {
  it("records the error on the match so a dead letter can name it", async () => {
    await insertMatch(ID, "queued");
    const result = await consume(new Error("D1 went away"));

    expect(result.retryMessages).toHaveLength(1);
    expect(events("consumer_error")[0]?.entry).toMatchObject({ match_id: ID, attempt: 1, error: "D1 went away" });
    expect(await matchRow(ID)).toMatchObject({ status: "queued", error: "D1 went away" });
  });
});

describe("dead_letter", () => {
  it("logs every message, even for an unknown match", async () => {
    const batch = createMessageBatch<ParseJob>("demoview-dev-parse-dlq", [
      { id: "msg-1", timestamp: new Date(), attempts: 3, body: { match_id: ID } },
    ]);
    await handleDeadLetters(batch, env);
    expect(events("dead_letter")).toEqual([
      {
        level: "error",
        entry: { event: "dead_letter", match_id: ID, attempts: 3, status: null, last_error: null },
      },
    ]);
  });
});

describe("API", () => {
  it("never logs the presigned URL or a secret", async () => {
    await handleRequest(request("POST", "/api/matches", { token: TOKEN }), env);
    const text = lines.map((l) => JSON.stringify(l.entry)).join("\n");
    for (const secret of ["X-Amz-Signature", "X-Amz-Credential", TOKEN, env.R2_SECRET_ACCESS_KEY]) {
      expect(text).not.toContain(secret);
    }
  });

  it("says why a confirm was rejected", async () => {
    await insertMatch(ID, "uploaded");
    const { env: e } = testEnv();
    const confirm = () => handleRequest(request("POST", `/api/matches/${ID}/uploaded`, { token: TOKEN }), e);

    await confirm();
    await env.BUCKET.put(`demos/${ID}.dem`, "PBDEMS2\0");
    await confirm();
    await confirm();

    expect(events("confirm_rejected").map((l) => l.entry)).toEqual([
      { event: "confirm_rejected", match_id: ID, reason: "demo_missing" },
      { event: "confirm_rejected", match_id: ID, reason: "wrong_status", status: "queued" },
    ]);
    expect(events("match_status").at(-1)?.entry).toMatchObject({ status: "queued", size_bytes: 8 });
  });

  it("flags a ready match with no match.json", async () => {
    await insertMatch(ID, "ready");
    await handleRequest(request("GET", `/api/matches/${ID}/match.json`), env);
    expect(events("object_missing")).toEqual([
      { level: "error", entry: { event: "object_missing", match_id: ID, key: `parsed/${ID}/match.json` } },
    ]);
  });
});
