import { describe, expect, it, vi } from "vitest";
import { httpParser, withFault, type Fetcher, type Parser } from "../src/parser";

const JOB = { match_id: "0123456789abc", demo_key: "demos/0123456789abc.dem", parse_id: "01k6gq3m1abcdefg" };

// A 200 from a parser that wrote the requested parse.
const ok = (body: unknown) => Response.json(body, { headers: { "x-parse-id": JOB.parse_id } });

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
    const { parser } = parserReturning(ok({ schema_version: 1 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "ok", status: 200, body: { schema_version: 1 } });
  });

  it.each([
    ["no parse id", undefined],
    ["another parse id", "01k6gq3m1zzzzzzz"],
  ])("treats a 200 with %s as transient", async (_, parseId) => {
    const headers: Record<string, string> = parseId ? { "x-parse-id": parseId } : {};
    const { parser } = parserReturning(Response.json({ schema_version: 1 }, { headers }));
    expect(await parser.parse(JOB)).toEqual({
      kind: "transient",
      status: 200,
      error: "parser did not write the requested parse",
    });
  });

  it("treats a 200 that is not JSON as transient", async () => {
    const { parser } = parserReturning(
      new Response("<html>", { status: 200, headers: { "x-parse-id": JOB.parse_id } }),
    );
    expect((await parser.parse(JOB)).kind).toBe("transient");
  });

  it("treats 422 as a rejected demo and keeps the parser's code and error", async () => {
    const body = { code: "demo_not_found", error: "no such demo" };
    const { parser } = parserReturning(Response.json(body, { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", status: 422, ...body });
  });

  it("calls a 422 without a code bad_demo", async () => {
    const { parser } = parserReturning(Response.json({ error: "not a CS2 demo" }, { status: 422 }));
    expect(await parser.parse(JOB)).toMatchObject({ code: "bad_demo", error: "not a CS2 demo" });
  });

  it("uses the raw body when a 422 is not JSON", async () => {
    const { parser } = parserReturning(new Response("bad magic", { status: 422 }));
    expect(await parser.parse(JOB)).toEqual({ kind: "rejected", status: 422, code: "bad_demo", error: "bad magic" });
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

describe("withFault", () => {
  const real: Parser = { parse: vi.fn(async () => ({ kind: "ok" as const, status: 200, body: {} })) };

  it.each([
    ["reject", { kind: "rejected", status: 422, code: "bad_demo", error: "fault injected" }],
    ["fail", { kind: "transient", status: 503, error: "fault injected" }],
  ])("FAULT=%s answers every parse without the parser", async (fault, result) => {
    expect(await withFault(real, fault).parse(JOB)).toEqual(result);
    expect(real.parse).not.toHaveBeenCalled();
  });

  it.each([undefined, "", "other"])("leaves the parser alone for %j", (fault) => {
    expect(withFault(real, fault)).toBe(real);
  });
});
