// Map styles. MapLibre needs literal colours, so the palettes below mirror
// styles/tokens.css. Two moods (dusk atlas, day field-notes) and a satellite base.
import type { LayerSpecification, StyleSpecification } from "maplibre-gl";
import { layers, namedFlavor, type Flavor } from "@protomaps/basemaps";

export type Mood = "dusk" | "day";

export const TRAIL_COLORS: Record<Mood, Record<string, string>> = {
  dusk: {
    usual: "#c9d3e0",
    shortest: "#4fd1ff",
    kaccha: "#ff8a4c",
    quiet: "#b48cff",
    scenic: "#4ee0a2",
    fresh: "#ffd23f",
    loop: "#ff6fae",
    open: "#3ec7ff",
    shore: "#7cf5e4",
  },
  day: {
    usual: "#59636f",
    shortest: "#0a8fc2",
    kaccha: "#d4581c",
    quiet: "#7a4fe0",
    scenic: "#13976a",
    fresh: "#c48f00",
    loop: "#d13c84",
    open: "#0a87c4",
    shore: "#11a39a",
  },
};

export const UI: Record<Mood, { accent: string; ink: string; paper: string; halo: string; ring: string; muted: string }> = {
  dusk: { accent: "#ffb547", ink: "#f4efe6", paper: "#0b0f15", halo: "#0b0f15", ring: "#ffb547", muted: "#7d786f" },
  day: { accent: "#d9771f", ink: "#1f1a14", paper: "#f4efe4", halo: "#fbf8f1", ring: "#c2541a", muted: "#8b8172" },
};

const GLYPHS = "https://protomaps.github.io/basemaps-assets/fonts/{fontstack}/{range}.pbf";
const SPRITE = (m: Mood) => `https://protomaps.github.io/basemaps-assets/sprites/v4/${m === "dusk" ? "dark" : "light"}`;
const TERRAIN = "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png";
const SATELLITE = "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}";
export const REMOTE_PMTILES = "https://demo-bucket.protomaps.com/v4.pmtiles";

/** Ink-blue atlas: land almost black, water deep teal, forests moss, roads graphite. */
function dusk(): Flavor {
  const f = namedFlavor("dark");
  const road = "#29323f";
  return {
    ...f,
    background: "#0a0e14",
    earth: "#0e131a",
    water: "#0e2a33",
    park_a: "#11201a",
    park_b: "#11201a",
    wood_a: "#10211a",
    wood_b: "#10211a",
    scrub_a: "#121c18",
    scrub_b: "#121c18",
    sand: "#1a1a16",
    beach: "#1a1a16",
    zoo: "#11201a",
    military: "#1a1416",
    hospital: "#15141c",
    school: "#14161d",
    industrial: "#12151b",
    pedestrian: "#141922",
    aerodrome: "#12161d",
    runway: "#1d2430",
    glacier: "#1c2430",
    buildings: "#151b24",
    other: "#1a212c",
    minor_service: "#181f29",
    minor_a: road,
    minor_b: road,
    link: "#334055",
    major: "#3a475d",
    highway: "#4d5d78",
    minor_casing: "#0e131a",
    minor_service_casing: "#0e131a",
    link_casing: "#0e131a",
    major_casing_early: "#0e131a",
    major_casing_late: "#0e131a",
    highway_casing_early: "#0e131a",
    highway_casing_late: "#0e131a",
    railway: "#2a2f3a",
    boundaries: "#3a4150",
    roads_label_minor: "#6d7584",
    roads_label_minor_halo: "#0e131a",
    roads_label_major: "#8b93a1",
    roads_label_major_halo: "#0e131a",
    ocean_label: "#3e6c78",
    subplace_label: "#7f8794",
    subplace_label_halo: "#0e131a",
    city_label: "#e9dfcc",
    city_label_halo: "#0a0e14",
    state_label: "#8b8577",
    state_label_halo: "#0a0e14",
    country_label: "#8b8577",
    address_label: "#5f6672",
    address_label_halo: "#0e131a",
    landcover: {
      barren: "#14161a",
      farmland: "#121813",
      forest: "#0f1d17",
      glacier: "#1c2430",
      grassland: "#111a15",
      scrub: "#121a16",
      urban_area: "#131820",
    },
  };
}

/** Field notes: warm paper, inked roads, soft greens and blues. */
function day(): Flavor {
  const f = namedFlavor("light");
  return {
    ...f,
    background: "#efe8da",
    earth: "#f2ecdf",
    water: "#b9d7dd",
    park_a: "#dce5c8",
    park_b: "#dce5c8",
    wood_a: "#cfdeb9",
    wood_b: "#cfdeb9",
    scrub_a: "#e1e6cd",
    scrub_b: "#e1e6cd",
    sand: "#ece2c8",
    beach: "#ece2c8",
    buildings: "#e4dccb",
    other: "#fffaf0",
    minor_service: "#fffaf0",
    minor_a: "#fffcf5",
    minor_b: "#fffcf5",
    link: "#fff6e4",
    major: "#fff3dc",
    highway: "#fde4b8",
    minor_casing: "#dcd1bd",
    minor_service_casing: "#dcd1bd",
    link_casing: "#d6c7ab",
    major_casing_early: "#d6c7ab",
    major_casing_late: "#d6c7ab",
    highway_casing_early: "#d9b77e",
    highway_casing_late: "#d9b77e",
    boundaries: "#b8ab95",
    city_label: "#2b241b",
    city_label_halo: "#f2ecdf",
    subplace_label: "#6b604f",
    subplace_label_halo: "#f2ecdf",
    roads_label_minor: "#8a7f6c",
    roads_label_minor_halo: "#fffcf5",
    roads_label_major: "#6b604f",
    roads_label_major_halo: "#fffcf5",
    state_label: "#9c907b",
    country_label: "#9c907b",
    landcover: {
      barren: "#ece4d3",
      farmland: "#e8e6cf",
      forest: "#d3e0bd",
      glacier: "#f4f4f2",
      grassland: "#e3e7cc",
      scrub: "#e2e5cc",
      urban_area: "#ebe3d4",
    },
  };
}

/** Fewer POI icons: Pagdandi draws its own gems. */
function tame(ls: LayerSpecification[]): LayerSpecification[] {
  return ls
    .filter((l) => l.id !== "pois")
    .map((l) => {
      if (l.id.startsWith("places_") && l.type === "symbol") {
        return { ...l, layout: { ...l.layout, "text-letter-spacing": 0.04 } } as LayerSpecification;
      }
      return l;
    });
}

export function buildStyle(opts: { mood: Mood; base: "atlas" | "satellite"; tilesUrl: string }): StyleSpecification {
  const { mood, base, tilesUrl } = opts;
  const flavor = mood === "dusk" ? dusk() : day();
  const sources: StyleSpecification["sources"] = {
    protomaps: {
      type: "vector",
      url: `pmtiles://${tilesUrl}`,
      attribution: '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org/copyright">OpenStreetMap</a>',
    },
    terrain: {
      type: "raster-dem",
      tiles: [TERRAIN],
      encoding: "terrarium",
      tileSize: 256,
      maxzoom: 14,
      attribution: "Terrain: Mapzen/AWS",
    },
  };
  let ls: LayerSpecification[];
  if (base === "satellite") {
    sources.satellite = {
      type: "raster",
      tiles: [SATELLITE],
      tileSize: 256,
      maxzoom: 19,
      attribution: "Imagery © Esri, Maxar, Earthstar Geographics",
    };
    ls = [
      { id: "background", type: "background", paint: { "background-color": "#0a0e14" } },
      { id: "satellite", type: "raster", source: "satellite", paint: { "raster-saturation": -0.1, "raster-contrast": 0.05, "raster-brightness-max": mood === "dusk" ? 0.72 : 0.95 } },
      ...tame(layers("protomaps", { ...flavor, city_label: "#fff6e6", city_label_halo: "#000000", subplace_label: "#f3eadb", subplace_label_halo: "#000000" }, { lang: "en", labelsOnly: true }) as LayerSpecification[]),
    ];
  } else {
    ls = tame(layers("protomaps", flavor, { lang: "en" }) as LayerSpecification[]);
    // Hillshade just under the roads: relief makes the atlas feel like terrain.
    const at = Math.max(1, ls.findIndex((l) => l.id.startsWith("roads_")));
    const hill: LayerSpecification = {
      id: "hillshade",
      type: "hillshade",
      source: "terrain",
      maxzoom: 16,
      paint:
        mood === "dusk"
          ? { "hillshade-exaggeration": 0.35, "hillshade-shadow-color": "#000000", "hillshade-highlight-color": "#2a3546", "hillshade-accent-color": "#05070a" }
          : { "hillshade-exaggeration": 0.3, "hillshade-shadow-color": "#8a7a5c", "hillshade-highlight-color": "#fffaf0", "hillshade-accent-color": "#6f6248" },
    };
    ls.splice(at, 0, hill);
  }
  return { version: 8, glyphs: GLYPHS, sprite: SPRITE(mood), sources, layers: ls };
}
