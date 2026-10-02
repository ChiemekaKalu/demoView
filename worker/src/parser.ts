// The parse service, behind an interface so tests can stub it.

export interface ParseRequest {
  match_id: string;
  demo_key: string;
  // The parse writes under parsed/{match_id}/{parse_id}/.
  parse_id: string;
}

// status is the parser's HTTP status, absent when it could not be reached.
export type ParseResult =
  // 200: the body is match.json, and every file of the parse, its manifest
  // last, is already in R2.
  | { kind: "ok"; status: number; body: unknown }
  // 422: the demo is bad. Don't retry. code says why, e.g. bad_demo.
  | { kind: "rejected"; status: number; code: string; error: string }
  // Anything else. Let the queue retry.
  | { kind: "transient"; status?: number; error: string };

// Which delivery of the queue message a parse is for, sent as headers so
// the parser's log lines can be matched to the Worker's.
export interface ParseAttempt {
  attempt: number;
  requestId: string;
}

export interface Parser {
  parse(request: ParseRequest, attempt?: ParseAttempt): Promise<ParseResult>;
}

// Stop waiting well inside the queue consumer's 15-minute limit. Parses take
// seconds.
const TIMEOUT_MS = 11 * 60 * 1000;

export type Fetcher = (input: URL, init: RequestInit) => Promise<Response>;

// Calls `POST /parse` on the parse service at baseUrl through fetcher: the
// container's Durable Object in production, any HTTP host otherwise.
export function httpParser(baseUrl: string, secret: string, fetcher: Fetcher): Parser {
  return {
    async parse(request, attempt) {
      const headers: Record<string, string> = { "content-type": "application/json", "x-parse-secret": secret };
      if (attempt) {
        headers["x-attempt"] = String(attempt.attempt);
        headers["x-request-id"] = attempt.requestId;
      }
      let res: Response;
      try {
        res = await fetcher(new URL("/parse", baseUrl), {
          method: "POST",
          headers,
          body: JSON.stringify(request),
          signal: AbortSignal.timeout(TIMEOUT_MS),
        });
      } catch (err) {
        return { kind: "transient", error: `parser unreachable: ${String(err)}` };
      }

      if (res.status === 200) {
        // A parser from before parse ids, still running through a rollout,
        // wrote somewhere else. The retry reaches the new one.
        if (res.headers.get("x-parse-id") !== request.parse_id) {
          await res.body?.cancel();
          return { kind: "transient", status: 200, error: "parser did not write the requested parse" };
        }
        try {
          return { kind: "ok", status: 200, body: await res.json() };
        } catch {
          return { kind: "transient", status: 200, error: "parser response was not valid JSON" };
        }
      }
      const { code, detail } = await errorBody(res);
      if (res.status === 422) {
        return {
          kind: "rejected",
          status: 422,
          code: code ?? "bad_demo",
          error: detail ?? "the parser rejected the demo",
        };
      }
      return {
        kind: "transient",
        status: res.status,
        error: `parser returned ${res.status}${detail ? `: ${detail}` : ""}`,
      };
    },
  };
}

// The `error` and `code` fields of a JSON error body. Without JSON, the
// detail is the start of the body text.
async function errorBody(res: Response): Promise<{ code?: string; detail?: string }> {
  const text = (await res.text().catch(() => "")).slice(0, 1000);
  try {
    const { error, code }: { error?: unknown; code?: unknown } = JSON.parse(text) ?? {};
    if (typeof error === "string") {
      return { detail: error, code: typeof code === "string" && /^[a-z_]{1,32}$/.test(code) ? code : undefined };
    }
  } catch {
    // Not JSON; fall through to the raw text.
  }
  return { detail: text || undefined };
}
