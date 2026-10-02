// What the viewer needs to draw a map's radar, from its HLTV overview file.

export interface Radar {
  // The world position of the image's top-left corner, and world units per
  // pixel of the 1024-pixel image.
  pos_x: number;
  pos_y: number;
  scale: number;
  // A map with a separate radar per floor, such as Nuke. Both floors are
  // drawn on its main radar until v1.2.
  two_floor: boolean;
}

// Reads an overview file (Valve KeyValues text, e.g. `"pos_x" "-3230"`).
// Returns null when a number is missing or unusable.
export function parseRadar(text: string): Radar | null {
  const num = (key: string) => Number(new RegExp(`"${key}"\\s+"(-?[\\d.]+)"`, "i").exec(text)?.[1]);
  const radar = {
    pos_x: num("pos_x"),
    pos_y: num("pos_y"),
    scale: num("scale"),
    two_floor: /"verticalsections"/i.test(text),
  };
  if (!Number.isFinite(radar.pos_x) || !Number.isFinite(radar.pos_y) || !(radar.scale > 0)) return null;
  return radar;
}
