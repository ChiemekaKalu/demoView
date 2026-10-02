import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { handleRequest } from "../src/api";
import { parseRadar } from "../src/radar";
import { request } from "./helpers";

// Trimmed from MurkyYT/cs2-map-icons data/radar_info/de_mirage.txt.
const MIRAGE = `"de_mirage"
{
	"material"	"overviews/de_mirage"	// texture file
	"pos_x"		"-3230"	// X coordinate,
	"pos_y"		"1713"	// Y coordinate,
	"scale"		"5.00" 	// and used scale used when taking the screenshot
	"rotate"	"0"
}`;

const NUKE = `"de_nuke"
{
	"pos_x"		"-3453"
	"pos_y"		"2887"
	"scale"		"7"
	"verticalsections"
	{
		"default" { "AltitudeMax" "10000" "AltitudeMin" "-495" }
		"lower" { "AltitudeMax" "-495" "AltitudeMin" "-10000" }
	}
}`;

const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);

async function putMap(name: string, info: string, image = true) {
  await env.BUCKET.put(`maps/radar_info/${name}.txt`, info);
  if (image) {
    await env.BUCKET.put(`maps/radars/${name}_radar_psd.png`, PNG, {
      httpMetadata: { contentType: "image/png" },
    });
  }
}

const get = (path: string, headers: Record<string, string> = {}) =>
  handleRequest(request("GET", path, { headers }), env);

describe("parseRadar", () => {
  it("reads the numbers from an overview file", () => {
    expect(parseRadar(MIRAGE)).toEqual({ pos_x: -3230, pos_y: 1713, scale: 5, two_floor: false });
  });

  it("marks a map with vertical sections as two-floor", () => {
    expect(parseRadar(NUKE)).toMatchObject({ scale: 7, two_floor: true });
  });

  it("returns null when a number is missing or unusable", () => {
    expect(parseRadar(MIRAGE.replace(/"pos_y".*\n/, ""))).toBeNull();
    expect(parseRadar(MIRAGE.replace('"5.00"', '"0"'))).toBeNull();
  });
});

describe("GET /api/maps/:name", () => {
  it("returns the radar numbers with a day-long cache", async () => {
    await putMap("de_mirage", MIRAGE);
    const res = await get("/api/maps/de_mirage");
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("public, max-age=86400");
    expect(await res.json()).toEqual({ name: "de_mirage", pos_x: -3230, pos_y: 1713, scale: 5 });
  });

  it.each([
    ["an unknown map", "de_nowhere"],
    ["a malformed name", "..%2Fsecret"],
  ])("returns 404 for %s", async (_, name) => {
    expect((await get(`/api/maps/${name}`)).status).toBe(404);
  });

  it("returns 404 when the map has no radar image", async () => {
    await putMap("de_dust", MIRAGE, false);
    expect(await (await get("/api/maps/de_dust")).json()).toEqual({ error: "no radar for this map" });
  });

  it("returns 404 for a two-floor map", async () => {
    await putMap("de_nuke", NUKE);
    const res = await get("/api/maps/de_nuke");
    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ error: "no radar for two-floor maps yet" });
  });
});

describe("GET /api/maps/:name/radar.png", () => {
  it("streams the image with its type, a day-long cache and an ETag", async () => {
    await putMap("de_mirage", MIRAGE);
    const res = await get("/api/maps/de_mirage/radar.png");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    expect(res.headers.get("cache-control")).toBe("public, max-age=86400");
    expect(new Uint8Array(await res.arrayBuffer())).toEqual(PNG);

    const etag = res.headers.get("etag") ?? "";
    expect((await get("/api/maps/de_mirage/radar.png", { "if-none-match": etag })).status).toBe(304);
  });

  it("returns 404 when there is no image", async () => {
    expect((await get("/api/maps/de_mirage/radar.png")).status).toBe(404);
  });
});
