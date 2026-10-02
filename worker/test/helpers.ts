import { env } from "cloudflare:workers";
import { vi } from "vitest";
import type { ParseJob } from "../src/api";

export const TOKEN = "test-upload-token";

export function request(
  method: string,
  path: string,
  { token, headers = {} }: { token?: string; headers?: Record<string, string> } = {},
): Request {
  const auth: Record<string, string> = token ? { authorization: `Bearer ${token}` } : {};
  return new Request(`https://api.test${path}`, { method, headers: { ...auth, ...headers } });
}

// The test env with the parse queue swapped for a recorder, so nothing is
// delivered to a consumer in the background.
export function testEnv(overrides: Partial<Env> = {}) {
  const sent: ParseJob[] = [];
  const queue = {
    send: vi.fn(async (job: ParseJob) => {
      sent.push(job);
    }),
    sendBatch: vi.fn(async (messages: { body: ParseJob }[]) => {
      sent.push(...messages.map((m) => m.body));
    }),
  };
  const testEnv = { ...env, PARSE_QUEUE: queue as unknown as Queue, ...overrides } as Env;
  return { env: testEnv, queue, sent };
}

export async function matchRow(id: string) {
  return env.DB.prepare("SELECT * FROM matches WHERE id = ?").bind(id).first<{
    status: string;
    error: string | null;
    demo_key: string;
    [column: string]: unknown;
  }>();
}

export async function insertMatch(id: string, status: string, extra: Record<string, unknown> = {}) {
  const row = { id, status, demo_key: `demos/${id}.dem`, created_at: 1, ...extra };
  const cols = Object.keys(row);
  await env.DB.prepare(
    `INSERT INTO matches (${cols.join(", ")}) VALUES (${cols.map(() => "?").join(", ")})`,
  )
    .bind(...Object.values(row))
    .run();
}

export async function events(matchId: string) {
  const { results } = await env.DB.prepare(
    "SELECT kind, msg, attempt, build, parse_id FROM match_events WHERE match_id = ? ORDER BY seq",
  )
    .bind(matchId)
    .all<{ kind: string; msg: string; attempt: number | null; build: string | null; parse_id: string | null }>();
  return results;
}
