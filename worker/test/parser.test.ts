import { describe, expect, it, vi } from "vitest";
import { cloudRunParser } from "../src/parser";

const JOB = { match_id: "0123456789abc", demo_key: "demos/0123456789abc.dem" };

function parserReturning(response: Response | Error) {
  const fetcher = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { parser: cloudRunParser("https://parser.test", "s3cret", fetcher as typeof fetch), fetcher };
}

describe("cloudRunParser", () => {
  it("POSTs the job to /parse with the shared secret", async () => {
    const { parser, fetcher } = parserReturning(Response.json({ ok: true }));
    await parser.parse(JOB);

    const [url, init] = fetcher.mock.calls[0] ?? [];
    expect(String(url)).toBe("https://parser.test/parse");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("x-parse-secret")).toBe("s3cret");
    expect(JSON.parse(String(init?.body))).toEqual(JOB);
  });

  it("returns the match.json body on 200", async () => {
    const { parser } = parserReturning(Response.json({ schema_version: 1 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "ok", body: { schema_version: 1 } });
  });

  it("treats a 200 that is not JSON as transient", async () => {
    const { parser } = parserReturning(new Response("<html>", { status: 200 }));
    expect((await parser.parse(JOB)).kind).toBe("transient");
  });

  it("treats 422 as a rejected demo and keeps the parser's error", async () => {
    const { parser } = parserReturning(Response.json({ error: "not a CS2 demo" }, { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", error: "not a CS2 demo" });
  });

  it("uses the raw body when a 422 is not JSON", async () => {
    const { parser } = parserReturning(new Response("bad magic", { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", error: "bad magic" });
  });

  it.each([500, 503, 401, 404])("treats %i as transient", async (status) => {
    const { parser } = parserReturning(new Response("nope", { status }));
    expect(await parser.parse(JOB)).toEqual({
      kind: "transient",
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
