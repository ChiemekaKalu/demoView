// The parser service, behind an interface so tests can stub it.

export interface ParseRequest {
  match_id: string;
  demo_key: string;
}

export type ParseResult =
  // 200: the body is match.json and the round files are already in R2.
  | { kind: "ok"; body: unknown }
  // 422: the demo is bad. Don't retry.
  | { kind: "rejected"; error: string }
  // Anything else. Let the queue retry.
  | { kind: "transient"; error: string };

export interface Parser {
  parse(request: ParseRequest): Promise<ParseResult>;
}

// Cloud Run gives up after 10 minutes. Stop waiting a little after that, well
// inside the queue consumer's 15-minute limit.
const TIMEOUT_MS = 11 * 60 * 1000;

// Calls `POST /parse` on the Cloud Run service.
export function cloudRunParser(baseUrl: string, secret: string, fetcher: typeof fetch = fetch): Parser {
  return {
    async parse(request) {
      let res: Response;
      try {
        res = await fetcher(new URL("/parse", baseUrl), {
          method: "POST",
          headers: { "content-type": "application/json", "x-parse-secret": secret },
          body: JSON.stringify(request),
          signal: AbortSignal.timeout(TIMEOUT_MS),
        });
      } catch (err) {
        return { kind: "transient", error: `parser unreachable: ${String(err)}` };
      }

      if (res.status === 200) {
        try {
          return { kind: "ok", body: await res.json() };
        } catch {
          return { kind: "transient", error: "parser response was not valid JSON" };
        }
      }
      const detail = await errorDetail(res);
      if (res.status === 422) {
        return { kind: "rejected", error: detail ?? "the parser rejected the demo" };
      }
      return {
        kind: "transient",
        error: `parser returned ${res.status}${detail ? `: ${detail}` : ""}`,
      };
    },
  };
}

// The `error` field of a JSON error body, else the start of the body text.
async function errorDetail(res: Response): Promise<string | undefined> {
  const text = (await res.text().catch(() => "")).slice(0, 1000);
  try {
    const error: unknown = JSON.parse(text)?.error;
    if (typeof error === "string") return error;
  } catch {
    // Not JSON; fall through to the raw text.
  }
  return text || undefined;
}
