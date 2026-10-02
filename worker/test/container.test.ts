import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { containerEnv } from "../src/container";

describe("containerEnv", () => {
  it("passes dev's round mode and shadow compare through", () => {
    expect(containerEnv({ ...env, PARSER_ROUNDS: "parser", PARSER_SHADOW: "oracle" })).toMatchObject({
      PARSER_ROUNDS: "parser",
      PARSER_SHADOW: "oracle",
      PARSER_BUILD: "test-build",
    });
  });

  it("never passes an empty value, which would override the image's default", () => {
    const vars = containerEnv({ ...env, PARSER_ROUNDS: "", PARSER_SHADOW: "" });
    expect(vars).not.toHaveProperty("PARSER_ROUNDS");
    expect(vars).not.toHaveProperty("PARSER_SHADOW");
    expect(vars).toHaveProperty("PARSE_SECRET");
  });

  it("passes prod's settings: the oracle's round files, no compare", () => {
    const vars = containerEnv({ ...env, PARSER_ROUNDS: "oracle", PARSER_SHADOW: "" });
    expect(vars).toMatchObject({ PARSER_ROUNDS: "oracle" });
    expect(vars).not.toHaveProperty("PARSER_SHADOW");
  });
});
