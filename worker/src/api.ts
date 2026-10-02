import { hasUploadToken } from "./auth";
import { demoKey, isMatchId, matchFileKey, newMatchId, roundKey } from "./keys";
import { errorStack, errorText } from "./log";
import { getMatch, getMatchPage, insertMatch, setStatus } from "./matches";
import { presignUpload, UPLOAD_URL_TTL_SECONDS } from "./presign";

// The message the API puts on the parse queue.
export interface ParseJob {
  match_id: string;
}

export const MAX_DEMO_BYTES = 1024 ** 3;

// Round files never change for a given parse, so browsers may keep them.
const ROUND_CACHE_CONTROL = "public, max-age=31536000, immutable";

// match.json lives at a fixed key that a reparse overwrites, so it is only
// cached briefly; the ETag makes revalidating it cheap.
const MATCH_FILE_CACHE_CONTROL = "public, max-age=300";

type Handler = (request: Request, env: Env, params: string[]) => Promise<Response>;

const routes: { method: string; pattern: RegExp; handler: Handler }[] = [
  { method: "POST", pattern: /^\/api\/matches$/, handler: createMatch },
  { method: "POST", pattern: /^\/api\/matches\/([^/]+)\/uploaded$/, handler: confirmUpload },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)$/, handler: getMatchSummary },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)\/match\.json$/, handler: getMatchFile },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)\/rounds\/([^/]+)$/, handler: getRoundFile },
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

  const object = await env.BUCKET.head(match.demo_key);
  // The only sign on this side that the browser's PUT to R2 failed.
  if (!object) {
    confirmRejected(id, "demo_missing");
    return error(409, "demo has not been uploaded");
  }
  const size = { size_bytes: object.size };
  if (object.size > MAX_DEMO_BYTES) {
    const message = "demo is larger than 1 GB";
    await env.BUCKET.delete(match.demo_key);
    await setStatus(env.DB, id, "failed", { error: message, from: ["uploaded"], log: size });
    return error(413, message);
  }

  // Claim the match before enqueueing, so two confirms can't enqueue twice.
  if (!(await setStatus(env.DB, id, "queued", { from: ["uploaded"], log: size }))) {
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
      players,
      rounds,
    },
    200,
    // The viewer polls this until the parse finishes.
    { "cache-control": "no-store" },
  );
}

// GET /api/matches/:id/rounds/:n
async function getRoundFile(
  request: Request,
  env: Env,
  [id = "", n = ""]: string[],
): Promise<Response> {
  if (!isMatchId(id) || !/^[1-9]\d{0,2}$/.test(n)) return error(404, "round not found");
  return (
    (await serveObject(request, env, roundKey(id, Number(n)), ROUND_CACHE_CONTROL)) ??
    error(404, "round not found")
  );
}

// GET /api/matches/:id/match.json: the parser's full match.json, for what
// D1 doesn't hold (kills with assisters and stable indexes, names).
async function getMatchFile(request: Request, env: Env, [id = ""]: string[]): Promise<Response> {
  const match = isMatchId(id) ? await getMatch(env.DB, id) : null;
  if (!match) return error(404, "match not found");
  // Before ready the object is missing, or is a reparse's half-written state.
  if (match.status !== "ready") return error(404, `match is ${match.status}, not ready`);
  const res = await serveObject(request, env, matchFileKey(id), MATCH_FILE_CACHE_CONTROL);
  if (res) return res;
  // A ready match always has one, so this is a bug or a lost object.
  console.error(JSON.stringify({ event: "object_missing", match_id: id, key: matchFileKey(id) }));
  return error(404, "match.json not found");
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
