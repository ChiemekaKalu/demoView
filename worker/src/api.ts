import { hasUploadToken } from "./auth";
import { addEvent, getEvents } from "./events";
import {
  demoKey,
  isMapName,
  isMatchId,
  isParseId,
  matchFileKey,
  newMatchId,
  radarImageKey,
  radarInfoKey,
  roundKey,
} from "./keys";
import { errorStack, errorText } from "./log";
import {
  claimUpload,
  deleteUploadedMatch,
  failure,
  getMatch,
  getMatchPage,
  insertMatch,
  setStatus,
} from "./matches";
import { presignUpload, UPLOAD_URL_TTL_SECONDS } from "./presign";
import { parseRadar } from "./radar";

// The message the API puts on the parse queue. A reparse parses a ready
// match again and keeps it ready whatever happens.
export interface ParseJob {
  match_id: string;
  reparse?: boolean;
}

export const MAX_DEMO_BYTES = 1024 ** 3;

// A parse's files never change, so a URL that names the parse is immutable.
const PARSE_CACHE_CONTROL = "public, max-age=31536000, immutable";

// A URL that reads the live parse changes when a reparse goes live, so it is
// only cached briefly; the ETag makes revalidating it cheap.
const LIVE_CACHE_CONTROL = "public, max-age=300";

// Map files change only when maps/ is uploaded again, so a day is long enough
// to cache them and short enough to pick up a fix.
const MAP_CACHE_CONTROL = "public, max-age=86400";

type Handler = (request: Request, env: Env, params: string[]) => Promise<Response>;

const routes: { method: string; pattern: RegExp; handler: Handler }[] = [
  { method: "POST", pattern: /^\/api\/matches$/, handler: createMatch },
  { method: "POST", pattern: /^\/api\/matches\/([^/]+)\/uploaded$/, handler: confirmUpload },
  { method: "POST", pattern: /^\/api\/matches\/([^/]+)\/reparse$/, handler: reparseMatch },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)$/, handler: getMatchSummary },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)\/events$/, handler: getMatchEvents },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)\/match\.json$/, handler: getMatchFile },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)\/rounds\/([^/]+)$/, handler: getRoundFile },
  { method: "GET", pattern: /^\/api\/maps\/([^/]+)$/, handler: getMap },
  { method: "GET", pattern: /^\/api\/maps\/([^/]+)\/radar\.png$/, handler: getRadarImage },
];

export async function handleRequest(request: Request, env: Env): Promise<Response> {
  const { pathname } = new URL(request.url);
  const allowed: string[] = [];
  for (const route of routes) {
    const m = route.pattern.exec(pathname);
    if (!m) continue;
    if (route.method !== request.method) {
      allowed.push(route.method);
      continue;
    }
    try {
      return await route.handler(request, env, m.slice(1));
    } catch (err) {
      console.error(
        JSON.stringify({ event: "unhandled_error", path: pathname, error: errorText(err), stack: errorStack(err) }),
      );
      return error(500, "internal error");
    }
  }
  if (allowed.length > 0) return error(405, "method not allowed", { allow: allowed.join(", ") });
  return error(404, "not found");
}

// POST /api/matches
async function createMatch(request: Request, env: Env): Promise<Response> {
  if (!(await hasUploadToken(request, env))) return unauthorized();
  const id = newMatchId();
  const key = demoKey(id);
  const uploadUrl = await presignUpload(env, key);
  await insertMatch(env.DB, id, key, Date.now());
  return json({ id, upload_url: uploadUrl, upload_expires_in: UPLOAD_URL_TTL_SECONDS }, 201);
}

// POST /api/matches/:id/uploaded
async function confirmUpload(request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  if (!(await hasUploadToken(request, env))) return unauthorized();
  const match = isMatchId(id) ? await getMatch(env.DB, id) : null;
  if (!match) return error(404, "match not found");
  if (match.status !== "uploaded") {
    confirmRejected(id, "wrong_status", { status: match.status });
    return json({ error: `match is already ${match.status}`, status: match.status }, 409);
  }

  const object = await env.BUCKET.get(match.demo_key);
  // The only sign on this side that the browser's PUT to R2 failed.
  if (!object) {
    confirmRejected(id, "demo_missing");
    return error(409, "demo has not been uploaded");
  }
  const size = { size_bytes: object.size };
  if (object.size > MAX_DEMO_BYTES) {
    await object.body.cancel();
    const message = "demo is larger than 1 GB";
    await env.BUCKET.delete(match.demo_key);
    await setStatus(env.DB, id, "failed", { error: failure("too_large", message), from: ["uploaded"], log: size });
    return error(413, message);
  }

  // Claim the match before enqueueing, so two confirms can't enqueue twice,
  // and reuse the match that already has this demo instead of parsing it again.
  const claim = await claimUpload(env.DB, id, await sha256Hex(object.body), size);
  if (claim.kind === "duplicate") {
    const original = claim.match;
    await env.BUCKET.delete(match.demo_key);
    await deleteUploadedMatch(env.DB, id);
    console.log(
      JSON.stringify({ event: "duplicate_upload", match_id: id, duplicate_of: original.id, status: original.status }),
    );
    return json({ id: original.id, status: original.status, duplicate: true }, 200);
  }
  if (claim.kind === "lost") {
    confirmRejected(id, "concurrent");
    return error(409, "match was confirmed concurrently");
  }
  try {
    await env.PARSE_QUEUE.send({ match_id: id } satisfies ParseJob);
  } catch (err) {
    await setStatus(env.DB, id, "uploaded", { from: ["queued"] });
    throw err;
  }
  return json({ id, status: "queued" }, 202);
}

// GET /api/matches/:id
async function getMatchSummary(_request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  const page = isMatchId(id) ? await getMatchPage(env.DB, id) : null;
  if (!page) return error(404, "match not found");
  const { match, players, rounds } = page;
  return json(
    {
      id: match.id,
      status: match.status,
      error: match.error,
      map: match.map,
      score_a: match.score_a,
      score_b: match.score_b,
      parser_version: match.parser_version,
      schema_version: match.schema_version,
      created_at: match.created_at,
      parsed_at: match.parsed_at,
      parse_id: match.live_parse_id,
      players,
      rounds,
    },
    200,
    // The viewer polls this until the parse finishes.
    { "cache-control": "no-store" },
  );
}

// POST /api/matches/:id/reparse: parses the demo again with the deployed
// parser. A ready match stays ready on its live parse until the new one
// replaces it. A failed match goes back in the queue.
async function reparseMatch(request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  if (!(await hasUploadToken(request, env))) return unauthorized();
  const match = isMatchId(id) ? await getMatch(env.DB, id) : null;
  if (!match) return error(404, "match not found");
  if (match.status !== "ready" && match.status !== "failed") {
    return json({ error: `match is ${match.status}`, status: match.status }, 409);
  }
  if (!(await env.BUCKET.head(match.demo_key))) return error(409, "the demo is no longer stored");

  if (match.status === "ready") {
    await env.PARSE_QUEUE.send({ match_id: id, reparse: true } satisfies ParseJob);
    console.log(JSON.stringify({ event: "reparse_queued", match_id: id, live_parse_id: match.live_parse_id }));
    await addEvent(env.DB, id, { kind: "reparse", msg: "requested through the API" });
    return json({ id, status: match.status }, 202);
  }
  if (!(await setStatus(env.DB, id, "queued", { error: null, from: ["failed"], log: { reparse: true } }))) {
    return error(409, "match was changed concurrently");
  }
  try {
    await env.PARSE_QUEUE.send({ match_id: id } satisfies ParseJob);
  } catch (err) {
    await setStatus(env.DB, id, "failed", { error: match.error, from: ["queued"] });
    throw err;
  }
  return json({ id, status: "queued" }, 202);
}

// GET /api/matches/:id/events: the match's pipeline history, oldest first.
async function getMatchEvents(_request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  const match = isMatchId(id) ? await getMatch(env.DB, id) : null;
  if (!match) return error(404, "match not found");
  return json({ events: await getEvents(env.DB, id) }, 200, { "cache-control": "no-store" });
}

// GET /api/matches/:id/rounds/:n
async function getRoundFile(
  request: Request,
  env: Env,
  [id = "", n = ""]: string[],
): Promise<Response> {
  if (!isMatchId(id) || !/^[1-9]\d{0,2}$/.test(n)) return error(404, "round not found");
  const parse = await parseToServe(request, env, id);
  if (parse instanceof Response) return parse;
  return (
    (await serveObject(request, env, roundKey(id, parse.id, Number(n)), parse.cacheControl)) ??
    error(404, "round not found")
  );
}

// GET /api/matches/:id/match.json: the parser's full match.json, for what
// D1 doesn't hold (kills with assisters and stable indexes, names).
async function getMatchFile(request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  if (!isMatchId(id)) return error(404, "match not found");
  const parse = await parseToServe(request, env, id);
  if (parse instanceof Response) return parse;
  const key = matchFileKey(id, parse.id);
  const res = await serveObject(request, env, key, parse.cacheControl);
  if (res) return res;
  // The live parse always has one, so this is a bug or a lost object. A
  // named parse may have been cleaned up.
  if (parse.live) console.error(JSON.stringify({ event: "object_missing", match_id: id, key }));
  return error(404, "match.json not found");
}

// Which parse a request for a match's files reads. ?v=<parse_id> names one,
// which needs no D1 read. Without it, the live parse, which only a ready match
// has. Its id is null for a match parsed before parses had their own prefix.
async function parseToServe(
  request: Request,
  env: Env,
  id: string,
): Promise<{ id: string | null; live: boolean; cacheControl: string } | Response> {
  const v = new URL(request.url).searchParams.get("v") ?? "";
  if (isParseId(v)) return { id: v, live: false, cacheControl: PARSE_CACHE_CONTROL };
  const match = await getMatch(env.DB, id);
  if (!match) return error(404, "match not found");
  if (match.status !== "ready") return error(404, `match is ${match.status}, not ready`);
  return { id: match.live_parse_id, live: true, cacheControl: LIVE_CACHE_CONTROL };
}

// GET /api/maps/:name: the numbers that place game positions on the map's
// radar image.
async function getMap(_request: Request, env: Env, [name = ""]: string[]): Promise<Response> {
  if (!isMapName(name)) return error(404, "no radar for this map");
  const [info, image] = await Promise.all([
    env.BUCKET.get(radarInfoKey(name)),
    env.BUCKET.head(radarImageKey(name)),
  ]);
  const radar = info ? parseRadar(await info.text()) : null;
  if (!radar || !image) return error(404, "no radar for this map");
  // One radar can't show two floors; that needs a layer per floor (v1.2).
  if (radar.two_floor) return error(404, "no radar for two-floor maps yet");
  const { pos_x, pos_y, scale } = radar;
  return json({ name, pos_x, pos_y, scale }, 200, { "cache-control": MAP_CACHE_CONTROL });
}

// GET /api/maps/:name/radar.png
async function getRadarImage(request: Request, env: Env, [name = ""]: string[]): Promise<Response> {
  if (!isMapName(name)) return error(404, "radar not found");
  return (
    (await serveObject(request, env, radarImageKey(name), MAP_CACHE_CONTROL)) ??
    error(404, "radar not found")
  );
}

// serveObject streams an R2 object with its ETag, or answers 304 when the
// request's If-None-Match matches. It returns null when the key is missing.
async function serveObject(
  request: Request,
  env: Env,
  key: string,
  cacheControl: string,
): Promise<Response | null> {
  const object = await env.BUCKET.get(key, { onlyIf: request.headers });
  if (!object) return null;

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  if (!headers.has("content-type")) headers.set("content-type", "application/json");
  headers.set("etag", object.httpEtag);
  headers.set("cache-control", cacheControl);
  // R2 returns the object without a body when If-None-Match matched.
  if (!("body" in object)) return new Response(null, { status: 304, headers });
  return new Response(object.body, { headers });
}

// A stream's SHA-256 as lowercase hex, without holding it in memory.
async function sha256Hex(stream: ReadableStream): Promise<string> {
  const digest = new crypto.DigestStream("SHA-256");
  await stream.pipeTo(digest);
  return [...new Uint8Array(await digest.digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function confirmRejected(id: string, reason: string, fields: Record<string, unknown> = {}): void {
  console.warn(JSON.stringify({ event: "confirm_rejected", match_id: id, reason, ...fields }));
}

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return Response.json(body, { status, headers });
}

function error(status: number, message: string, headers: Record<string, string> = {}): Response {
  return json({ error: message }, status, headers);
}

function unauthorized(): Response {
  return error(401, "missing or wrong upload token", { "www-authenticate": "Bearer" });
}
