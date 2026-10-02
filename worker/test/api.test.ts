import { env, exports } from "cloudflare:workers";
import { describe, expect, it, vi } from "vitest";
import { handleRequest, MAX_DEMO_BYTES } from "../src/api";
import { insertMatch, matchRow, request, testEnv, TOKEN } from "./helpers";

const ID = "0123456789abc";

describe("routing", () => {
  it("serves the API through the Worker's default export", async () => {
    const res = await exports.default.fetch(request("GET", `/api/matches/${ID}`));
    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ error: "match not found" });
  });

  it("returns 405 with Allow for a known path and wrong method", async () => {
    const res = await handleRequest(request("PUT", "/api/matches"), env);
    expect(res.status).toBe(405);
    expect(res.headers.get("allow")).toBe("POST");
  });

  it("returns 404 for unknown paths", async () => {
    const res = await handleRequest(request("GET", "/nope"), env);
    expect(res.status).toBe(404);
  });
});

describe("POST /api/matches", () => {
  it.each([
    ["no token", undefined],
    ["a wrong token", "wrong"],
  ])("rejects %s and creates nothing", async (_, token) => {
    const res = await handleRequest(request("POST", "/api/matches", { token }), env);
    expect(res.status).toBe(401);
    expect(res.headers.get("www-authenticate")).toBe("Bearer");
    const count = await env.DB.prepare("SELECT count(*) AS n FROM matches").first<number>("n");
    expect(count).toBe(0);
  });

  it("creates an uploaded match and returns a presigned PUT URL", async () => {
    const res = await handleRequest(request("POST", "/api/matches", { token: TOKEN }), env);
    expect(res.status).toBe(201);
    const body = await res.json<{ id: string; upload_url: string; upload_expires_in: number }>();
    expect(body.id).toMatch(/^[0-9a-z]{13}$/);
    expect(body.upload_expires_in).toBe(3600);

    const row = await matchRow(body.id);
    expect(row).toMatchObject({ status: "uploaded", demo_key: `demos/${body.id}.dem` });

    const url = new URL(body.upload_url);
    expect(url.host).toBe("testaccount.r2.cloudflarestorage.com");
    expect(url.pathname).toBe(`/demoview-test/demos/${body.id}.dem`);
    const q = url.searchParams;
    expect(q.get("X-Amz-Algorithm")).toBe("AWS4-HMAC-SHA256");
    expect(q.get("X-Amz-Credential")).toMatch(/^test-access-key\/\d{8}\/auto\/s3\/aws4_request$/);
    expect(q.get("X-Amz-Expires")).toBe("3600");
    expect(q.get("X-Amz-SignedHeaders")).toBe("host");
    expect(q.get("X-Amz-Signature")).toMatch(/^[0-9a-f]{64}$/);
  });

  it("gives every match a different id", async () => {
    const ids = new Set<string>();
    for (let i = 0; i < 5; i++) {
      const res = await handleRequest(request("POST", "/api/matches", { token: TOKEN }), env);
      ids.add((await res.json<{ id: string }>()).id);
    }
    expect(ids.size).toBe(5);
  });
});

describe("POST /api/matches/:id/uploaded", () => {
  const confirm = (e: Env, id = ID, token: string | null = TOKEN) =>
    handleRequest(request("POST", `/api/matches/${id}/uploaded`, { token: token ?? undefined }), e);

  it("requires the upload token", async () => {
    await insertMatch(ID, "uploaded");
    const { env: e, sent } = testEnv();
    expect((await confirm(e, ID, null)).status).toBe(401);
    expect(sent).toEqual([]);
  });

  it.each([ID, "not-an-id"])("returns 404 for unknown match %s", async (id) => {
    const { env: e } = testEnv();
    expect((await confirm(e, id)).status).toBe(404);
  });

  it("returns 409 and stays uploaded when the demo is missing from R2", async () => {
    await insertMatch(ID, "uploaded");
    const { env: e, sent } = testEnv();
    const res = await confirm(e);
    expect(res.status).toBe(409);
    expect(await res.json()).toEqual({ error: "demo has not been uploaded" });
    expect((await matchRow(ID))?.status).toBe("uploaded");
    expect(sent).toEqual([]);
  });

  it("queues the match and enqueues one parse job", async () => {
    await insertMatch(ID, "uploaded");
    await env.BUCKET.put(`demos/${ID}.dem`, "PBDEMS2\0");
    const { env: e, sent } = testEnv();

    const res = await confirm(e);
    expect(res.status).toBe(202);
    expect(await res.json()).toEqual({ id: ID, status: "queued" });
    expect((await matchRow(ID))?.status).toBe("queued");
    expect(sent).toEqual([{ match_id: ID }]);

    const again = await confirm(e);
    expect(again.status).toBe(409);
    expect(await again.json()).toEqual({ error: "match is already queued", status: "queued" });
    expect(sent).toHaveLength(1);
  });

  it("rejects a demo over 1 GB, deletes it and fails the match", async () => {
    await insertMatch(ID, "uploaded");
    const bucket = {
      head: vi.fn(async () => ({ size: MAX_DEMO_BYTES + 1 })),
      delete: vi.fn(async () => {}),
    };
    const { env: e, sent } = testEnv({ BUCKET: bucket as unknown as R2Bucket });

    const res = await confirm(e);
    expect(res.status).toBe(413);
    expect(bucket.delete).toHaveBeenCalledWith(`demos/${ID}.dem`);
    expect(await matchRow(ID)).toMatchObject({ status: "failed", error: "demo is larger than 1 GB" });
    expect(sent).toEqual([]);
  });

  it("accepts a demo of exactly 1 GB", async () => {
    await insertMatch(ID, "uploaded");
    const bucket = { head: vi.fn(async () => ({ size: MAX_DEMO_BYTES })) };
    const { env: e, sent } = testEnv({ BUCKET: bucket as unknown as R2Bucket });
    expect((await confirm(e)).status).toBe(202);
    expect(sent).toHaveLength(1);
  });

  it("puts the match back to uploaded if enqueueing fails", async () => {
    await insertMatch(ID, "uploaded");
    await env.BUCKET.put(`demos/${ID}.dem`, "PBDEMS2\0");
    const queue = { send: vi.fn(async () => Promise.reject(new Error("queue down"))) };
    const { env: e } = testEnv({ PARSE_QUEUE: queue as unknown as Queue });

    const res = await confirm(e);
    expect(res.status).toBe(500);
    expect((await matchRow(ID))?.status).toBe("uploaded");
  });
});

describe("GET /api/matches/:id", () => {
  const get = (id = ID) => handleRequest(request("GET", `/api/matches/${id}`), env);

  it("returns 404 for unknown or malformed ids", async () => {
    expect((await get()).status).toBe(404);
    expect((await get("..%2F..%2Fsecret")).status).toBe(404);
  });

  it("returns status with no scoreboard before the parse", async () => {
    await insertMatch(ID, "parsing");
    const res = await get();
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toMatchObject({
      id: ID,
      status: "parsing",
      error: null,
      map: null,
      players: [],
      rounds: [],
    });
  });

  it("returns the scoreboard and rounds in order, without Steam ids", async () => {
    await insertMatch(ID, "ready", {
      map: "de_mirage",
      score_a: 13,
      score_b: 9,
      parser_version: "0.1.0",
      schema_version: 1,
      parsed_at: 2,
    });
    const players = env.DB.prepare(
      "INSERT INTO players VALUES (?, ?, '7656', ?, ?, 20, 10, 5, 90.5, 0.5)",
    );
    const rounds = env.DB.prepare("INSERT INTO rounds VALUES (?, ?, 'A', 'bomb_defused', 'CT')");
    await env.DB.batch([
      players.bind(ID, 1, "b", "B"),
      players.bind(ID, 0, "a", "A"),
      rounds.bind(ID, 2),
      rounds.bind(ID, 1),
    ]);

    const body = await (await get()).json<Record<string, unknown>>();
    expect(body).toMatchObject({
      status: "ready",
      map: "de_mirage",
      score_a: 13,
      score_b: 9,
      parser_version: "0.1.0",
      schema_version: 1,
      parsed_at: 2,
    });
    expect(body.players).toEqual([
      { idx: 0, name: "a", team: "A", kills: 20, deaths: 10, assists: 5, adr: 90.5, hs_pct: 0.5 },
      { idx: 1, name: "b", team: "B", kills: 20, deaths: 10, assists: 5, adr: 90.5, hs_pct: 0.5 },
    ]);
    expect(body.rounds).toEqual([
      { n: 1, winner: "A", reason: "bomb_defused", side_a: "CT" },
      { n: 2, winner: "A", reason: "bomb_defused", side_a: "CT" },
    ]);
  });
});

describe("GET /api/matches/:id/rounds/:n", () => {
  const get = (path: string, headers: Record<string, string> = {}) =>
    handleRequest(request("GET", path, { headers }), env);
  const roundFile = JSON.stringify({ schema_version: 1, round: 7 });

  it.each(["7", "0", "07", "x", "1000"])("returns 404 for missing or invalid round %s", async (n) => {
    expect((await get(`/api/matches/${ID}/rounds/${n}`)).status).toBe(404);
  });

  it("streams the round file with long-lived cache headers", async () => {
    await env.BUCKET.put(`parsed/${ID}/rounds/07.json`, roundFile);
    const res = await get(`/api/matches/${ID}/rounds/7`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("application/json");
    expect(res.headers.get("cache-control")).toBe("public, max-age=31536000, immutable");
    expect(res.headers.get("etag")).toBeTruthy();
    expect(await res.text()).toBe(roundFile);
  });

  it("pads round numbers to two digits and keeps three-digit ones", async () => {
    await env.BUCKET.put(`parsed/${ID}/rounds/100.json`, roundFile);
    expect((await get(`/api/matches/${ID}/rounds/100`)).status).toBe(200);
  });

  it("returns 304 when the ETag matches", async () => {
    await env.BUCKET.put(`parsed/${ID}/rounds/07.json`, roundFile);
    const etag = (await get(`/api/matches/${ID}/rounds/7`)).headers.get("etag") ?? "";
    const res = await get(`/api/matches/${ID}/rounds/7`, { "if-none-match": etag });
    expect(res.status).toBe(304);
    expect(res.headers.get("etag")).toBe(etag);
  });
});

describe("GET /api/matches/:id/match.json", () => {
  const path = `/api/matches/${ID}/match.json`;
  const get = (headers: Record<string, string> = {}) => handleRequest(request("GET", path, { headers }), env);
  const matchFile = JSON.stringify({ schema_version: 1, kills: [] });

  it("returns 404 for unknown or malformed ids", async () => {
    expect((await get()).status).toBe(404);
    expect((await handleRequest(request("GET", "/api/matches/x/match.json"), env)).status).toBe(404);
  });

  it("returns 404 until the match is ready, even if the object exists", async () => {
    await insertMatch(ID, "parsing");
    await env.BUCKET.put(`parsed/${ID}/match.json`, matchFile);
    const res = await get();
    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ error: "match is parsing, not ready" });
  });

  it("returns 404 when a ready match has no match.json", async () => {
    await insertMatch(ID, "ready");
    expect((await get()).status).toBe(404);
  });

  it("streams match.json with a short cache and an ETag", async () => {
    await insertMatch(ID, "ready");
    await env.BUCKET.put(`parsed/${ID}/match.json`, matchFile);
    const res = await get();
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("application/json");
    expect(res.headers.get("cache-control")).toBe("public, max-age=300");
    expect(await res.text()).toBe(matchFile);

    const etag = res.headers.get("etag") ?? "";
    expect(etag).toBeTruthy();
    expect((await get({ "if-none-match": etag })).status).toBe(304);
  });
});
