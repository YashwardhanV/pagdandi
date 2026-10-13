import type { Flavour, TrailKind, VehicleId } from "./api";

/** What each trail kind means, in one breath. Colours live in tokens.css (--k-*). */
export const KIND_META: Record<TrailKind, { label: string; hi: string; blurb: string }> = {
  usual: { label: "The Usual", hi: "हमेशा वाला", blurb: "The way everyone goes" },
  shortest: { label: "Shortcut", hi: "छोटा रास्ता", blurb: "Fewest kilometres, whatever the road" },
  kaccha: { label: "Kaccha", hi: "कच्चा रास्ता", blurb: "Dirt, gravel, village roads" },
  quiet: { label: "Quiet Lanes", hi: "सुनसान गलियाँ", blurb: "Away from the busy roads" },
  scenic: { label: "Scenic", hi: "नज़ारा", blurb: "Bends toward water, forest and views" },
  fresh: { label: "New Ground", hi: "नई ज़मीन", blurb: "Roads new on the map" },
  loop: { label: "Loop", hi: "चक्कर", blurb: "Out one way, back another" },
  open: { label: "Open Water", hi: "खुला पानी", blurb: "Straight across, away from the banks" },
  shore: { label: "Shoreline", hi: "किनारा", blurb: "Hugs the shore for the view" },
};

export const kindColor = (k: TrailKind) => `var(--k-${k})`;
export const trailColor = (t: { kind: TrailKind; tint?: TrailKind }) => kindColor(t.tint ?? t.kind);
export const LOOP_TINTS: TrailKind[] = ["loop", "scenic", "shortest"];

export const VEHICLE_ORDER: VehicleId[] = ["bike", "scooty", "car", "cycle", "walk", "jetski"];

export const FLAVOURS: { id: Flavour; label: string; hint: string }[] = [
  { id: "any", label: "Anything", hint: "Surprise me" },
  { id: "water", label: "Water", hint: "Lakes, falls, rivers" },
  { id: "hills", label: "Hills", hint: "Peaks and viewpoints" },
  { id: "forest", label: "Forest", hint: "Green and wild" },
  { id: "heritage", label: "Heritage", hint: "Forts, palaces, old stones" },
  { id: "wild", label: "Offbeat", hint: "Caves, falls, back of beyond" },
  { id: "fresh", label: "New on map", hint: "Roads mapped recently" },
];

export const PLACE_KIND: Record<string, { label: string; tint: string }> = {
  waterfall: { label: "Waterfall", tint: "var(--k-open)" },
  lake: { label: "Lake", tint: "var(--k-open)" },
  reservoir: { label: "Reservoir", tint: "var(--k-open)" },
  river: { label: "River", tint: "var(--k-open)" },
  dam: { label: "Dam", tint: "var(--k-open)" },
  ghat: { label: "Ghat", tint: "var(--k-shore)" },
  spring: { label: "Spring", tint: "var(--k-shore)" },
  viewpoint: { label: "Viewpoint", tint: "var(--k-fresh)" },
  peak: { label: "Hilltop", tint: "var(--k-fresh)" },
  cliff: { label: "Cliff", tint: "var(--k-fresh)" },
  fort: { label: "Fort", tint: "var(--k-kaccha)" },
  palace: { label: "Palace", tint: "var(--k-kaccha)" },
  ruins: { label: "Ruins", tint: "var(--k-kaccha)" },
  monument: { label: "Monument", tint: "var(--k-kaccha)" },
  temple: { label: "Temple", tint: "var(--k-kaccha)" },
  cave: { label: "Cave", tint: "var(--k-quiet)" },
  wild: { label: "Wildlife", tint: "var(--k-scenic)" },
  park: { label: "Park", tint: "var(--k-scenic)" },
  picnic: { label: "Picnic", tint: "var(--k-scenic)" },
  camp: { label: "Camp", tint: "var(--k-scenic)" },
  attraction: { label: "Local spot", tint: "var(--k-loop)" },
  fresh: { label: "New road", tint: "var(--k-fresh)" },
};

export const placeKind = (k: string) => PLACE_KIND[k] ?? { label: k.charAt(0).toUpperCase() + k.slice(1), tint: "var(--k-loop)" };
