import { hasUploadToken } from "./auth";
import { demoKey, isMatchId, newMatchId, roundKey } from "./keys";
import { getMatch, getMatchPage, insertMatch, setStatus } from "./matches";
import { presignUpload, UPLOAD_URL_TTL_SECONDS } from "./presign";

// The message the API puts on the parse queue.
export interface ParseJob {
  match_id: string;
}

export const MAX_DEMO_BYTES = 1024 ** 3;

// Round files never change for a given parse, so browsers may keep them.
const ROUND_CACHE_CONTROL = "public, max-age=31536000, immutable";

type Handler = (request: Request, env: Env, params: string[]) => Promise<Response>;

const routes: { method: string; pattern: RegExp; handler: Handler }[] = [
  { method: "POST", pattern: /^\/api\/matches$/, handler: createMatch },
  { method: "POST", pattern: /^\/api\/matches\/([^/]+)\/uploaded$/, handler: confirmUpload },
  { method: "GET", pattern: /^\/api\/matches\/([^/]+)$/, handler: getMatchSummary },
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
      console.error(JSON.stringify({ event: "unhandled_error", path: pathname, error: String(err) }));
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
    return json({ error: `match is already ${match.status}`, status: match.status }, 409);
  }

  const object = await env.BUCKET.head(match.demo_key);
  if (!object) return error(409, "demo has not been uploaded");
  if (object.size > MAX_DEMO_BYTES) {
    const message = "demo is larger than 1 GB";
    await env.BUCKET.delete(match.demo_key);
    await setStatus(env.DB, id, "failed", { error: message, from: ["uploaded"] });
    return error(413, message);
  }

  // Claim the match before enqueueing, so two confirms can't enqueue twice.
  if (!(await setStatus(env.DB, id, "queued", { from: ["uploaded"] }))) {
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
  const object = await env.BUCKET.get(roundKey(id, Number(n)), { onlyIf: request.headers });
  if (!object) return error(404, "round not found");

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  if (!headers.has("content-type")) headers.set("content-type", "application/json");
  headers.set("etag", object.httpEtag);
  headers.set("cache-control", ROUND_CACHE_CONTROL);
  // R2 returns the object without a body when If-None-Match matched.
  if (!("body" in object)) return new Response(null, { status: 304, headers });
  return new Response(object.body, { headers });
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
