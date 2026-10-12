// Map styles (SPEC §11.3). The only file besides tokens.css allowed raw colours:
// MapLibre paints with literal colours, so the palette below mirrors tokens.css.
import type { StyleSpecification, LayerSpecification } from "maplibre-gl";
import { layers, namedFlavor, type Flavor } from "@protomaps/basemaps";

export const PALETTE = {
  light: {
    accent: "#C2410C",
    standard: "#3B4A5A",
    casing: "#FFFFFF",
    ink: "#17191C",
    halo: "#FFFFFF",
    warn: "#B45309",
    danger: "#B42318",
    ok: "#2F7D32",
    pending: "#6B7079",
  },
  dark: {
    accent: "#FF7A3D",
    standard: "#9AA8B8",
    casing: "#0E1012",
    ink: "#ECEDEE",
    halo: "#0E1012",
    warn: "#E0913A",
    danger: "#F0705F",
    ok: "#5DB462",
    pending: "#8F959C",
  },
} as const;
export type Scheme = keyof typeof PALETTE;

const GLYPHS = "https://protomaps.github.io/basemaps-assets/fonts/{fontstack}/{range}.pbf";
const SPRITE = (s: Scheme) => `https://protomaps.github.io/basemaps-assets/sprites/v4/${s}`;

/** Lower saturation, our water/park colours, white roads with a warm casing. */
function flavor(scheme: Scheme): Flavor {
  const base = namedFlavor(scheme === "light" ? "light" : "dark");
  if (scheme === "dark") {
    return { ...base, water: "#1C2A31", park_a: "#1E2620", park_b: "#1E2620", wood_a: "#1E2620", wood_b: "#1E2620" };
  }
  return {
    ...base,
    background: "#F2F1EE",
    earth: "#F2F1EE",
    water: "#CFDFE6",
    park_a: "#DCE6D3",
    park_b: "#DCE6D3",
    wood_a: "#DCE6D3",
    wood_b: "#DCE6D3",
    scrub_a: "#E3E8DA",
    scrub_b: "#E3E8DA",
    minor_a: "#FFFFFF",
    minor_b: "#FFFFFF",
    minor_service: "#FFFFFF",
    major: "#FFFFFF",
    highway: "#FFFFFF",
    link: "#FFFFFF",
    minor_casing: "#D9D5CE",
    minor_service_casing: "#D9D5CE",
    major_casing_early: "#D9D5CE",
    major_casing_late: "#D9D5CE",
    highway_casing_early: "#D9D5CE",
    highway_casing_late: "#D9D5CE",
    link_casing: "#D9D5CE",
    buildings: "#E7E4DF",
  };
}

/** Halve POI density and hide minor POIs below z15. */
function tamePois(ls: LayerSpecification[]): LayerSpecification[] {
  return ls.map((l) => {
    if (l.id === "pois" && l.type === "symbol") {
      return { ...l, minzoom: 15, layout: { ...l.layout, "text-padding": 12, "icon-padding": 12 } } as LayerSpecification;
    }
    return l;
  });
}

/**
 * Local style: self-hosted Protomaps PMTiles. India labels use English names:
 * MapLibre cannot shape Devanagari, so local scripts would render broken
 * (docs/DECISIONS.md). Sweden labels use the Swedish name.
 */
export function protomapsStyle(tilesUrl: string, scheme: Scheme, country: string): StyleSpecification {
  const lang = country === "SE" ? "sv" : "en";
  return {
    version: 8,
    glyphs: GLYPHS,
    sprite: SPRITE(scheme),
    sources: {
      protomaps: {
        type: "vector",
        url: `pmtiles://${tilesUrl}`,
        attribution: '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org/copyright">OpenStreetMap contributors</a>',
      },
    },
    layers: tamePois(layers("protomaps", flavor(scheme), { lang }) as LayerSpecification[]),
  };
}

/** Remote fallback (dev, or no local tiles): OpenFreeMap hosted styles, recoloured. */
export const REMOTE_STYLE = {
  light: "https://tiles.openfreemap.org/styles/positron",
  dark: "https://tiles.openfreemap.org/styles/dark",
};

export async function remoteStyle(scheme: Scheme): Promise<StyleSpecification> {
  const res = await fetch(REMOTE_STYLE[scheme]);
  const style = (await res.json()) as StyleSpecification;
  if (scheme === "light") {
    style.layers = style.layers.map((l) => {
      if (l.type === "fill" && /water/.test(l.id)) return { ...l, paint: { ...l.paint, "fill-color": "#CFDFE6" } } as LayerSpecification;
      if (l.type === "fill" && /park|wood|grass/.test(l.id)) return { ...l, paint: { ...l.paint, "fill-color": "#DCE6D3" } } as LayerSpecification;
      return l;
    });
  }
  return style;
}
