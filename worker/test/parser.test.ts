import { describe, expect, it, vi } from "vitest";
import { httpParser, type Fetcher } from "../src/parser";

const JOB = { match_id: "0123456789abc", demo_key: "demos/0123456789abc.dem" };

function parserReturning(response: Response | Error) {
  const fetcher = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { parser: httpParser("https://parser.test", "s3cret", fetcher as Fetcher), fetcher };
}

describe("httpParser", () => {
  it("POSTs the job to /parse with the shared secret", async () => {
    const { parser, fetcher } = parserReturning(Response.json({ ok: true }));
    await parser.parse(JOB);

    const [url, init] = fetcher.mock.calls[0] ?? [];
    expect(String(url)).toBe("https://parser.test/parse");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("x-parse-secret")).toBe("s3cret");
    expect(JSON.parse(String(init?.body))).toEqual(JOB);
  });

  it("sends the attempt and request id when given", async () => {
    const { parser, fetcher } = parserReturning(Response.json({}));
    await parser.parse(JOB, { attempt: 2, requestId: "msg-1" });
    const headers = new Headers(fetcher.mock.calls[0]?.[1]?.headers);
    expect(headers.get("x-attempt")).toBe("2");
    expect(headers.get("x-request-id")).toBe("msg-1");
  });

  it("returns the match.json body on 200", async () => {
    const { parser } = parserReturning(Response.json({ schema_version: 1 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "ok", status: 200, body: { schema_version: 1 } });
  });

  it("treats a 200 that is not JSON as transient", async () => {
    const { parser } = parserReturning(new Response("<html>", { status: 200 }));
    expect((await parser.parse(JOB)).kind).toBe("transient");
  });

  it("treats 422 as a rejected demo and keeps the parser's error", async () => {
    const { parser } = parserReturning(Response.json({ error: "not a CS2 demo" }, { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", status: 422, error: "not a CS2 demo" });
  });

  it("uses the raw body when a 422 is not JSON", async () => {
    const { parser } = parserReturning(new Response("bad magic", { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", status: 422, error: "bad magic" });
  });

  it.each([500, 503, 401, 404])("treats %i as transient", async (status) => {
    const { parser } = parserReturning(new Response("nope", { status }));
    expect(await parser.parse(JOB)).toEqual({
      kind: "transient",
      status,
      error: `parser returned ${status}: nope`,
    });
  });

  it("treats a network error as transient", async () => {
    const { parser } = parserReturning(new TypeError("connection refused"));
    const result = await parser.parse(JOB);
    expect(result.kind).toBe("transient");
    expect(result).toMatchObject({ error: expect.stringContaining("connection refused") });
  });
});
