import { createScheduledController } from "cloudflare:test";
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { handleRequest } from "../src/api";
import { cleanup, reparseBatch, sweep } from "../src/cron";
import worker from "../src/index";
import { newParseId } from "../src/keys";
import { CONTAINER_MS_PER_MONTH } from "../src/usage";
import { events, insertMatch, matchRow, request, testEnv, TOKEN } from "./helpers";

const ID = "0123456789abc";
// Status changes are stamped with the real clock, so the tests run at it.
const NOW = Date.now();
const MINUTE = 60 * 1000;
const DAY = 24 * 60 * MINUTE;

afterEach(() => {
  vi.restoreAllMocks();
});

describe("sweep", () => {
  it("requeues a match stuck in queued for 30 minutes", async () => {
    await insertMatch(ID, "queued", { status_changed_at: NOW - 31 * MINUTE });
    const { env: e, sent } = testEnv();
    await sweep(e, NOW);
    expect(sent).toEqual([{ match_id: ID }]);
    const row = await matchRow(ID);
    expect(row?.status).toBe("queued");
    expect(row?.status_changed_at).toBeGreaterThan(NOW - 31 * MINUTE);
    expect((await events(ID)).at(-1)?.msg).toBe("queued (no change for 30 minutes in queued)");
  });

  it("fails a match stuck in parsing", async () => {
    await insertMatch(ID, "parsing", { status_changed_at: NOW - 31 * MINUTE });
    const { env: e, sent } = testEnv();
    await sweep(e, NOW);
    expect(sent).toEqual([]);
    expect(await matchRow(ID)).toMatchObject({
      status: "failed",
      error: "stuck: no result after 30 minutes of parsing",
    });
  });

  it.each<[string, number]>([
    ["queued", NOW - 29 * MINUTE],
    ["parsing", NOW - 29 * MINUTE],
    ["ready", NOW - DAY],
    ["uploaded", NOW - DAY],
  ])("leaves a %s match changed at %i alone", async (status, changedAt) => {
    await insertMatch(ID, status, { status_changed_at: changedAt });
    const { env: e, sent } = testEnv();
    await sweep(e, NOW);
    expect(sent).toEqual([]);
    expect(await matchRow(ID)).toMatchObject({ status, status_changed_at: changedAt });
  });
});

describe("reparseBatch", () => {
  const ready = (id: string, createdAt: number, build: string | null) =>
    insertMatch(id, "ready", { created_at: createdAt, attempted_build: build });

  it("queues five ready matches from older builds, oldest first, and marks them", async () => {
    for (let i = 0; i < 7; i++) await ready(`${i}`.repeat(13), 10 - i, i === 6 ? "test-build" : i % 2 ? "old" : null);
    const { env: e, sent } = testEnv();
    await reparseBatch(e, NOW);

    // 6 is on this build already; of the rest, 5 and 4 are the oldest.
    const ids = ["5555555555555", "4444444444444", "3333333333333", "2222222222222", "1111111111111"];
    expect(sent).toEqual(ids.map((match_id) => ({ match_id, reparse: true })));
    for (const id of ids) {
      expect((await matchRow(id))?.attempted_build).toBe("test-build");
      expect(await events(id)).toEqual([
        { kind: "reparse", msg: "queued by the hourly reparse", attempt: null, build: "test-build", parse_id: null },
      ]);
    }
    expect((await matchRow("0000000000000"))?.attempted_build).toBeNull();
  });

  it("waits while a match is queued or parsing", async () => {
    await ready(ID, 1, null);
    await insertMatch("1111111111111", "parsing");
    const { env: e, sent } = testEnv();
    await reparseBatch(e, NOW);
    expect(sent).toEqual([]);
  });

  it("stops for the month at 80% of the container allowance", async () => {
    await ready(ID, 1, null);
    const month = new Date(NOW);
    await env.DB.prepare("INSERT INTO container_usage VALUES (?, ?)")
      .bind(month.toISOString().slice(0, 7), 0.8 * CONTAINER_MS_PER_MONTH)
      .run();
    const { env: e, sent } = testEnv();
    await reparseBatch(e, NOW);
    expect(sent).toEqual([]);

    // A new month starts from zero.
    await reparseBatch(e, Date.UTC(month.getUTCFullYear(), month.getUTCMonth() + 1, 1));
    expect(sent).toEqual([{ match_id: ID, reparse: true }]);
  });

  it("does nothing on a deploy without a build id", async () => {
    await ready(ID, 1, null);
    const { env: e, sent } = testEnv({ PARSER_BUILD: "dev" });
    await reparseBatch(e, NOW);
    expect(sent).toEqual([]);
  });
});

describe("cleanup", () => {
  const OLD = newParseId(NOW - 8 * DAY);
  const LIVE = newParseId(NOW - 8 * DAY + MINUTE);
  const YOUNG = newParseId(NOW - DAY);

  const put = (...keys: string[]) => Promise.all(keys.map((k) => env.BUCKET.put(k, "{}")));
  const keys = async () => (await env.BUCKET.list({ prefix: "parsed/" })).objects.map((o) => o.key).sort();
  const attempt = (parseId: string, ts: number) =>
    env.DB.prepare(
      "INSERT INTO match_events VALUES (?1, (SELECT COUNT(*) + 1 FROM match_events WHERE match_id = ?1), ?2, 'attempt', 'x', 1, 'b', ?3)",
    )
      .bind(ID, ts, parseId)
      .run();

  it("deletes week-old parses that aren't live, once", async () => {
    await insertMatch(ID, "ready", { created_at: NOW - 9 * DAY, live_parse_id: LIVE });
    await attempt(OLD, NOW - 8 * DAY);
    await attempt(LIVE, NOW - 8 * DAY + MINUTE);
    await attempt(YOUNG, NOW - DAY);
    await put(
      `parsed/${ID}/${OLD}/match.json`,
      `parsed/${ID}/${OLD}/rounds/01.json`,
      `parsed/${ID}/${LIVE}/match.json`,
      `parsed/${ID}/${YOUNG}/match.json`,
    );

    const cleanups = async () => (await events(ID)).filter((e) => e.kind === "cleanup" && e.parse_id);
    await cleanup(env, NOW);
    expect(await keys()).toEqual([`parsed/${ID}/${LIVE}/match.json`, `parsed/${ID}/${YOUNG}/match.json`]);
    expect(await cleanups()).toEqual([
      { kind: "cleanup", msg: "removed 2 files", attempt: null, build: null, parse_id: OLD },
    ]);

    await cleanup(env, NOW);
    expect(await cleanups()).toHaveLength(1);
  });

  it("deletes a reparsed match's v1 files by key once its live parse is a week old", async () => {
    // Created before the first recorded attempt, so it may have v1 files.
    await insertMatch(ID, "ready", { created_at: NOW - 30 * DAY, live_parse_id: LIVE });
    await attempt(LIVE, NOW - 8 * DAY + MINUTE);
    await put(
      `parsed/${ID}/match.json`,
      `parsed/${ID}/rounds/01.json`,
      `parsed/${ID}/rounds/02.json`,
      `parsed/${ID}/${LIVE}/match.json`,
      `parsed/${ID}/${LIVE}/rounds/01.json`,
    );

    await cleanup(env, NOW - 2 * DAY); // the live parse is only six days old
    expect(await keys()).toHaveLength(5);

    await cleanup(env, NOW);
    expect(await keys()).toEqual([`parsed/${ID}/${LIVE}/match.json`, `parsed/${ID}/${LIVE}/rounds/01.json`]);
    expect((await events(ID)).at(-1)).toMatchObject({ kind: "cleanup", msg: "removed 3 v1 files", parse_id: null });
  });

  it("deletes uploads that were never confirmed after a day", async () => {
    await insertMatch(ID, "uploaded", { created_at: NOW - DAY - MINUTE });
    await insertMatch("1111111111111", "uploaded", { created_at: NOW - MINUTE });
    await env.BUCKET.put(`demos/${ID}.dem`, "PBDEMS2\0");

    await cleanup(env, NOW);
    expect(await matchRow(ID)).toBeNull();
    expect(await env.BUCKET.head(`demos/${ID}.dem`)).toBeNull();
    expect(await matchRow("1111111111111")).not.toBeNull();
  });
});

describe("scheduled handler", () => {
  it("runs the job for each cron", async () => {
    await insertMatch(ID, "parsing", { status_changed_at: NOW - 31 * MINUTE });
    const controller = createScheduledController({ cron: "*/10 * * * *", scheduledTime: NOW });
    await worker.scheduled(controller, env);
    expect((await matchRow(ID))?.status).toBe("failed");
  });
});

describe("POST /api/admin/cron/:job", () => {
  const run = (job: string, token = TOKEN) => handleRequest(request("POST", `/api/admin/cron/${job}`, { token }), env);

  it("runs the job now", async () => {
    await insertMatch(ID, "parsing", { status_changed_at: Date.now() - 31 * MINUTE });
    const res = await run("sweep");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ job: "sweep", ran: true });
    expect((await matchRow(ID))?.status).toBe("failed");
  });

  it("requires the upload token", async () => {
    await insertMatch(ID, "parsing", { status_changed_at: Date.now() - 31 * MINUTE });
    expect((await run("sweep", "")).status).toBe(401);
    expect((await matchRow(ID))?.status).toBe("parsing");
  });

  it.each(["nope", "constructor"])("returns 404 for an unknown job %s", async (job) => {
    expect((await run(job)).status).toBe(404);
  });
});
