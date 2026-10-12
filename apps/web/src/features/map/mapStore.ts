import { create } from "zustand";
import type { Map as MLMap, MapMouseEvent } from "maplibre-gl";
import type { LatLng } from "../../lib/geo";

/** Draft geometry shown while drawing or recording a shortcut. */
export type Draft = { points: LatLng[]; snaps: ({ ok: boolean } | null)[] };

type MapState = {
  map: MLMap | null;
  /** Geometry to highlight (shortcut detail, candidate prompt). */
  highlight: { polyline: string; kind: "shortcut" | "candidate" } | null;
  draft: Draft | null;
  /** Click handler installed by the current screen (draw mode). */
  onClick: ((p: LatLng, e: MapMouseEvent) => void) | null;
  /** Bounds request; MapView fits to it when it changes. */
  fit: { bounds: [[number, number], [number, number]]; padding?: number; key: number } | null;
  follow: boolean; // keep the user dot centred (navigation)
  set: (p: Partial<MapState>) => void;
  fitTo: (bounds: [[number, number], [number, number]], padding?: number) => void;
};

export const useMapStore = create<MapState>((set) => ({
  map: null,
  highlight: null,
  draft: null,
  onClick: null,
  fit: null,
  follow: false,
  set: (p) => set(p),
  fitTo: (bounds, padding) => set({ fit: { bounds, padding, key: Date.now() } }),
}));
