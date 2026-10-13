import { useEffect, useRef } from "react";
import maplibregl, { type GeoJSONSource, type LngLatBoundsLike, type Map as MLMap } from "maplibre-gl";
import { Protocol } from "pmtiles";
import "maplibre-gl/dist/maplibre-gl.css";
import { api, type Gem, type Place, type Trail } from "../lib/api";
import { circle, destination } from "../lib/geo";
import { origin, selectedTrail, useStore, type State } from "../state/store";
import { buildStyle, REMOTE_PMTILES, TRAIL_COLORS, UI, type Mood } from "./style";

let protocolAdded = false;
const EMPTY: GeoJSON.FeatureCollection = { type: "FeatureCollection", features: [] };

/** Trails currently on screen, depending on mode. */
function visibleTrails(s: State): Trail[] {
  if (s.mode === "ride") return s.ride.trail ? [s.ride.trail] : [];
  if (s.mode === "wander") return s.wander.tab === "loops" ? s.wander.result?.loops ?? [] : [];
  return s.go.result?.trails ?? [];
}

function trailsFC(s: State, mood: Mood): GeoJSON.FeatureCollection {
  const ts = visibleTrails(s);
  const sel = selectedTrail(s);
  const colors = TRAIL_COLORS[mood];
  // Selected trail drawn last (on top).
  const ordered = [...ts].sort((a, b) => Number(a.id === sel?.id) - Number(b.id === sel?.id));
  return {
    type: "FeatureCollection",
    features: ordered.map((t) => ({
      type: "Feature",
      id: t.id,
      properties: { id: t.id, kind: t.kind, color: colors[t.tint ?? t.kind] ?? colors.usual, selected: t.id === sel?.id ? 1 : 0 },
      geometry: { type: "LineString", coordinates: t.geometry },
    })),
  };
}

function viaFC(s: State): GeoJSON.FeatureCollection {
  const t = s.mode === "ride" ? s.ride.trail : selectedTrail(s);
  if (!t || s.mode === "wander" && s.wander.tab === "places") return EMPTY;
  const feats: GeoJSON.Feature[] = [];
  for (const v of t.via ?? []) {
    if (!v.name) continue;
    feats.push({ type: "Feature", properties: { name: v.name }, geometry: { type: "Point", coordinates: [v.lng, v.lat] } });
  }
  return { type: "FeatureCollection", features: feats };
}

function gemsFC(gems: Gem[]): GeoJSON.FeatureCollection {
  return {
    type: "FeatureCollection",
    features: gems.map((g) => ({
      type: "Feature",
      properties: { id: g.id, name: g.name, kind: g.kind, featured: g.featured ? 1 : 0, score: g.score },
      geometry: { type: "Point", coordinates: [g.lng, g.lat] },
    })),
  };
}

function placesFC(places: Place[]): GeoJSON.FeatureCollection {
  return {
    type: "FeatureCollection",
    features: places.map((p, i) => ({
      type: "Feature",
      properties: { id: p.id, name: p.name, kind: p.kind, rank: i + 1 },
      geometry: { type: "Point", coordinates: [p.lng, p.lat] },
    })),
  };
}

function radiusFC(s: State): GeoJSON.FeatureCollection {
  const o = origin(s);
  if (s.mode !== "wander" || !o) return EMPTY;
  const r = s.wander.radiusKm * 1000;
  const top = destination(o.at, 0, r);
  return {
    type: "FeatureCollection",
    features: [
      { type: "Feature", properties: { kind: "ring" }, geometry: { type: "Polygon", coordinates: [circle(o.at, r)] } },
      { type: "Feature", properties: { kind: "label", label: `${s.wander.radiusKm} km` }, geometry: { type: "Point", coordinates: [top.lng, top.lat] } },
    ],
  };
}

function regionFC(s: State): GeoJSON.FeatureCollection {
  if (!s.meta) return EMPTY;
  return { type: "FeatureCollection", features: [{ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: s.meta.region.ring } }] };
}

function setData(map: MLMap, id: string, data: GeoJSON.FeatureCollection) {
  // The map may have been removed (StrictMode remount) or be mid-restyle.
  if (!(map as unknown as { style?: unknown }).style) return;
  try {
    (map.getSource(id) as GeoJSONSource | undefined)?.setData(data);
  } catch {
    /* source not ready yet; the next style.load refreshes it */
  }
}

function addOverlays(map: MLMap, mood: Mood) {
  const ui = UI[mood];
  const src = (id: string) => {
    if (!map.getSource(id)) map.addSource(id, { type: "geojson", data: EMPTY });
  };
  ["region", "fresh", "radius", "gems", "places", "trails", "via", "progress"].forEach(src);

  map.addLayer({ id: "region-line", type: "line", source: "region", paint: { "line-color": ui.muted, "line-width": 1.2, "line-dasharray": [2, 3], "line-opacity": 0.6 } });

  // Wander radius: a soft lens with a dashed rim and its distance on top.
  map.addLayer({ id: "radius-fill", type: "fill", source: "radius", filter: ["==", ["get", "kind"], "ring"], paint: { "fill-color": ui.accent, "fill-opacity": mood === "dusk" ? 0.06 : 0.07 } });
  map.addLayer({ id: "radius-glow", type: "line", source: "radius", filter: ["==", ["get", "kind"], "ring"], paint: { "line-color": ui.accent, "line-width": 10, "line-blur": 10, "line-opacity": 0.35 } });
  map.addLayer({ id: "radius-line", type: "line", source: "radius", filter: ["==", ["get", "kind"], "ring"], paint: { "line-color": ui.accent, "line-width": 1.6, "line-dasharray": [3, 2] } });
  map.addLayer({
    id: "radius-label",
    type: "symbol",
    source: "radius",
    filter: ["==", ["get", "kind"], "label"],
    layout: { "text-field": ["get", "label"], "text-font": ["Noto Sans Medium"], "text-size": 13, "text-offset": [0, -0.9], "text-allow-overlap": true },
    paint: { "text-color": ui.accent, "text-halo-color": ui.halo, "text-halo-width": 2 },
  });

  map.addLayer({
    id: "fresh-line",
    type: "line",
    source: "fresh",
    minzoom: 10,
    layout: { "line-cap": "round" },
    paint: { "line-color": TRAIL_COLORS[mood].fresh, "line-width": ["interpolate", ["linear"], ["zoom"], 10, 1.2, 15, 3.5], "line-dasharray": [1, 1.6], "line-opacity": 0.85 },
  });

  // Trails: blurred glow, dark casing, coloured core, then an animated flow on the selected one.
  map.addLayer({
    id: "trail-glow",
    type: "line",
    source: "trails",
    layout: { "line-cap": "round", "line-join": "round" },
    paint: {
      "line-color": ["get", "color"],
      "line-width": ["case", ["==", ["get", "selected"], 1], 18, 0],
      "line-blur": 14,
      "line-opacity": mood === "dusk" ? 0.55 : 0.3,
    },
  });
  map.addLayer({
    id: "trail-casing",
    type: "line",
    source: "trails",
    layout: { "line-cap": "round", "line-join": "round" },
    paint: {
      "line-color": mood === "dusk" ? "#05070a" : "#fffaf0",
      "line-width": ["interpolate", ["linear"], ["zoom"], 8, ["case", ["==", ["get", "selected"], 1], 5, 3], 15, ["case", ["==", ["get", "selected"], 1], 11, 7]],
      "line-opacity": ["case", ["==", ["get", "selected"], 1], 0.9, 0.55],
    },
  });
  map.addLayer({
    id: "trail-line",
    type: "line",
    source: "trails",
    layout: { "line-cap": "round", "line-join": "round" },
    paint: {
      "line-color": ["get", "color"],
      "line-width": ["interpolate", ["linear"], ["zoom"], 8, ["case", ["==", ["get", "selected"], 1], 3.2, 1.8], 15, ["case", ["==", ["get", "selected"], 1], 7, 4]],
      "line-opacity": ["case", ["==", ["get", "selected"], 1], 1, 0.5],
    },
  });
  map.addLayer({
    id: "trail-flow",
    type: "line",
    source: "trails",
    filter: ["==", ["get", "selected"], 1],
    layout: { "line-cap": "butt", "line-join": "round" },
    paint: {
      "line-color": mood === "dusk" ? "#ffffff" : "#fffaf0",
      "line-width": ["interpolate", ["linear"], ["zoom"], 8, 1.2, 15, 2.6],
      "line-opacity": 0.75,
      "line-dasharray": [0, 4, 3],
    },
  });
  map.addLayer({
    id: "progress-line",
    type: "line",
    source: "progress",
    layout: { "line-cap": "round", "line-join": "round" },
    paint: { "line-color": mood === "dusk" ? "#3a4252" : "#b9ad98", "line-width": ["interpolate", ["linear"], ["zoom"], 10, 4, 16, 9] },
  });

  // Gems: glowing dots, names when there's room.
  const kindColor: maplibregl.ExpressionSpecification = [
    "match",
    ["get", "kind"],
    ["waterfall", "lake", "dam", "reservoir", "river"],
    TRAIL_COLORS[mood].open,
    ["viewpoint", "peak", "cliff"],
    TRAIL_COLORS[mood].fresh,
    ["fort", "palace", "ruins", "monument", "temple"],
    TRAIL_COLORS[mood].kaccha,
    ["wild", "park", "picnic", "camp"],
    TRAIL_COLORS[mood].scenic,
    ["cave"],
    TRAIL_COLORS[mood].quiet,
    TRAIL_COLORS[mood].loop,
  ];
  map.addLayer({
    id: "gems-halo",
    type: "circle",
    source: "gems",
    minzoom: 8,
    filter: ["any", ["==", ["get", "featured"], 1], [">=", ["zoom"], 10.5]],
    paint: { "circle-color": kindColor, "circle-radius": ["interpolate", ["linear"], ["zoom"], 8, 7, 14, 14], "circle-blur": 1, "circle-opacity": mood === "dusk" ? 0.5 : 0.3 },
  });
  map.addLayer({
    id: "gems-dot",
    type: "circle",
    source: "gems",
    minzoom: 8,
    filter: ["any", ["==", ["get", "featured"], 1], [">=", ["zoom"], 10.5]],
    paint: {
      "circle-color": kindColor,
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 8, ["case", ["==", ["get", "featured"], 1], 3.5, 2.5], 14, ["case", ["==", ["get", "featured"], 1], 6, 4.5]],
      "circle-stroke-color": ui.halo,
      "circle-stroke-width": 1.5,
    },
  });
  map.addLayer({
    id: "gems-label",
    type: "symbol",
    source: "gems",
    minzoom: 9,
    filter: ["any", ["==", ["get", "featured"], 1], [">=", ["zoom"], 12]],
    layout: {
      "text-field": ["get", "name"],
      "text-font": ["Noto Sans Italic"],
      "text-size": ["interpolate", ["linear"], ["zoom"], 9, 11, 14, 13],
      "text-offset": [0, 1.1],
      "text-anchor": "top",
      "text-max-width": 9,
      "symbol-sort-key": ["-", 1, ["get", "score"]],
    },
    paint: { "text-color": ui.ink, "text-halo-color": ui.halo, "text-halo-width": 1.6, "text-opacity": 0.92 },
  });

  // Wander places: numbered beacons.
  map.addLayer({ id: "places-halo", type: "circle", source: "places", paint: { "circle-color": ui.accent, "circle-radius": 16, "circle-blur": 1, "circle-opacity": 0.45 } });
  map.addLayer({
    id: "places-dot",
    type: "circle",
    source: "places",
    paint: { "circle-color": ui.accent, "circle-radius": 9.5, "circle-stroke-color": ui.halo, "circle-stroke-width": 2 },
  });
  map.addLayer({
    id: "places-num",
    type: "symbol",
    source: "places",
    layout: { "text-field": ["to-string", ["get", "rank"]], "text-font": ["Noto Sans Medium"], "text-size": 11, "text-allow-overlap": true },
    paint: { "text-color": mood === "dusk" ? "#1b1206" : "#fff8ee" },
  });
  map.addLayer({
    id: "places-label",
    type: "symbol",
    source: "places",
    layout: { "text-field": ["get", "name"], "text-font": ["Noto Sans Medium"], "text-size": 12, "text-offset": [0, 1.3], "text-anchor": "top", "text-max-width": 9, "text-optional": true },
    paint: { "text-color": ui.ink, "text-halo-color": ui.halo, "text-halo-width": 1.8 },
  });

  map.addLayer({
    id: "via-dot",
    type: "circle",
    source: "via",
    paint: { "circle-color": ui.halo, "circle-radius": 5, "circle-stroke-color": ui.ink, "circle-stroke-width": 2 },
  });
  map.addLayer({
    id: "via-label",
    type: "symbol",
    source: "via",
    layout: { "text-field": ["get", "name"], "text-font": ["Noto Sans Italic"], "text-size": 12, "text-offset": [0, -1.1], "text-anchor": "bottom", "text-max-width": 10 },
    paint: { "text-color": ui.ink, "text-halo-color": ui.halo, "text-halo-width": 1.8 },
  });
}

function markerEl(cls: string, label?: string): HTMLElement {
  // MapLibre owns the element's transform, so the visual lives in a child.
  const el = document.createElement("div");
  el.className = `pin ${cls}`;
  const shape = document.createElement("span");
  shape.className = "pin-shape";
  el.appendChild(shape);
  if (label) {
    const t = document.createElement("span");
    t.className = "pin-label";
    t.textContent = label;
    el.appendChild(t);
  }
  return el;
}

function sheetPadding(): maplibregl.PaddingOptions {
  const desktop = window.matchMedia("(min-width: 900px)").matches;
  if (desktop) return { top: 90, bottom: 60, left: 460, right: 60 };
  const sheet = document.querySelector<HTMLElement>("[data-sheet]");
  const h = sheet ? Math.min(window.innerHeight * 0.62, sheet.getBoundingClientRect().height) : window.innerHeight * 0.45;
  return { top: 110, bottom: h + 40, left: 36, right: 64 };
}

export function MapView() {
  const el = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MLMap | null>(null);
  const gemsRef = useRef<Gem[]>([]);
  const freshRef = useRef<GeoJSON.FeatureCollection>(EMPTY);
  const markers = useRef<{ from?: maplibregl.Marker; to?: maplibregl.Marker; me?: maplibregl.Marker }>({});
  const styleKey = useRef("");

  // Create the map once meta is known (tiles URL).
  const meta = useStore((s) => s.meta);
  useEffect(() => {
    if (!el.current || mapRef.current || !meta) return;
    if (!protocolAdded) {
      maplibregl.addProtocol("pmtiles", new Protocol().tile);
      protocolAdded = true;
    }
    const s = useStore.getState();
    const tilesUrl = meta.tiles.available ? new URL(meta.tiles.url, window.location.origin).toString() : REMOTE_PMTILES;
    const c = s.me ?? meta.region.center;
    const map = new maplibregl.Map({
      container: el.current,
      style: buildStyle({ mood: s.resolvedTheme, base: s.base, tilesUrl }),
      center: [c.lng, c.lat],
      zoom: s.me ? 13.5 : 11.2,
      minZoom: 6,
      maxZoom: 18.5,
      maxPitch: 70,
      attributionControl: { compact: true },
      pixelRatio: Math.min(window.devicePixelRatio, 2),
      fadeDuration: 0,
    });
    map.touchZoomRotate.disableRotation();
    map.dragRotate.disable();
    mapRef.current = map;
    styleKey.current = `${s.resolvedTheme}|${s.base}`;
    (map as unknown as { __tiles: string }).__tiles = tilesUrl;

    map.on("style.load", () => {
      const st = useStore.getState();
      addOverlays(map, st.resolvedTheme);
      refresh(map);
      applyTerrain(map, st.terrain3d);
    });

    // Clicks: trails select, gems/places open.
    map.on("click", (e) => {
      const st = useStore.getState();
      const feats = map.queryRenderedFeatures(e.point, { layers: ["trail-line", "places-dot", "gems-dot", "gems-label"].filter((l) => map.getLayer(l)) });
      const f = feats[0];
      if (!f) return;
      if (f.layer.id === "trail-line" && st.mode !== "ride") st.select(String(f.properties?.id));
      else if (f.layer.id === "places-dot") {
        const p = st.wander.result?.places?.find((x) => x.id === f.properties?.id);
        if (p) st.goToPlace(p);
      } else if (f.layer.id.startsWith("gems")) {
        const g = gemsRef.current.find((x) => x.id === f.properties?.id);
        if (g && st.mode !== "ride") st.setTo({ name: g.name, sub: g.kind, at: g.access ?? { lat: g.lat, lng: g.lng }, kind: g.kind });
      }
    });
    map.on("mouseenter", "trail-line", () => (map.getCanvas().style.cursor = "pointer"));
    map.on("mouseleave", "trail-line", () => (map.getCanvas().style.cursor = ""));
    for (const l of ["gems-dot", "places-dot"]) {
      map.on("mouseenter", l, () => (map.getCanvas().style.cursor = "pointer"));
      map.on("mouseleave", l, () => (map.getCanvas().style.cursor = ""));
    }
    // Long-press / right-click: go here.
    map.on("contextmenu", async (e) => {
      const st = useStore.getState();
      if (st.mode === "ride") return;
      const at = { lat: e.lngLat.lat, lng: e.lngLat.lng };
      st.setTo({ name: "Dropped pin", sub: `${at.lat.toFixed(4)}, ${at.lng.toFixed(4)}`, at });
      try {
        const p = await api.reverse(at);
        const cur = useStore.getState().to;
        if (cur && cur.at === at) useStore.setState({ to: { ...cur, name: p.name, sub: p.label || cur.sub } });
      } catch {
        /* keep the pin name */
      }
    });

    // New-on-the-map roads for the visible area.
    let freshAbort: AbortController | null = null;
    const loadFresh = async () => {
      const st = useStore.getState();
      const want = st.showFresh || (st.mode === "wander" && st.wander.flavour === "fresh");
      if (!want || map.getZoom() < 10) {
        if (freshRef.current.features.length) {
          freshRef.current = EMPTY;
          setData(map, "fresh", EMPTY);
        }
        return;
      }
      const b = map.getBounds();
      freshAbort?.abort();
      freshAbort = new AbortController();
      try {
        const fc = await api.fresh([b.getWest(), b.getSouth(), b.getEast(), b.getNorth()], freshAbort.signal);
        freshRef.current = fc;
        setData(map, "fresh", fc);
      } catch {
        /* ignore */
      }
    };
    map.on("moveend", loadFresh);
    const unsubFresh = useStore.subscribe((st, prev) => {
      if (st.showFresh !== prev.showFresh || st.wander.flavour !== prev.wander.flavour || st.mode !== prev.mode) void loadFresh();
    });

    void api.gems().then((r) => {
      gemsRef.current = r.gems;
      setData(map, "gems", gemsFC(r.gems));
    });

    // Flowing dashes on the selected trail.
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const seq = [
      [0, 4, 3],
      [0.5, 4, 2.5],
      [1, 4, 2],
      [1.5, 4, 1.5],
      [2, 4, 1],
      [2.5, 4, 0.5],
      [3, 4, 0],
      [0, 0.5, 3, 3.5],
      [0, 1, 3, 3],
      [0, 1.5, 3, 2.5],
      [0, 2, 3, 2],
      [0, 2.5, 3, 1.5],
      [0, 3, 3, 1],
      [0, 3.5, 3, 0.5],
    ];
    let step = -1;
    let raf = 0;
    const animate = (t: number) => {
      const i = Math.floor(t / 70) % seq.length;
      if (i !== step && map.getLayer("trail-flow")) {
        map.setPaintProperty("trail-flow", "line-dasharray", seq[i]);
        step = i;
      }
      raf = requestAnimationFrame(animate);
    };
    if (!reduce) raf = requestAnimationFrame(animate);

    return () => {
      cancelAnimationFrame(raf);
      unsubFresh();
      map.remove();
      mapRef.current = null;
    };

    function refresh(m: MLMap) {
      const st = useStore.getState();
      const mood = st.resolvedTheme;
      setData(m, "region", regionFC(st));
      setData(m, "trails", trailsFC(st, mood));
      setData(m, "via", viaFC(st));
      setData(m, "radius", radiusFC(st));
      setData(m, "places", st.mode === "wander" && st.wander.tab === "places" ? placesFC(st.wander.result?.places ?? []) : EMPTY);
      setData(m, "gems", gemsFC(gemsRef.current));
      setData(m, "fresh", freshRef.current);
    }
  }, [meta]);

  // Restyle on mood/base change.
  const mood = useStore((s) => s.resolvedTheme);
  const base = useStore((s) => s.base);
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const key = `${mood}|${base}`;
    if (key === styleKey.current) return;
    styleKey.current = key;
    const tiles = (map as unknown as { __tiles: string }).__tiles;
    map.setStyle(buildStyle({ mood, base, tilesUrl: tiles }), { diff: false });
  }, [mood, base]);

  // Keep overlay data in sync with state.
  useEffect(() => {
    return useStore.subscribe((st, prev) => {
      const map = mapRef.current;
      if (!map || !map.getSource("trails")) return;
      if (st.go !== prev.go || st.wander !== prev.wander || st.selected !== prev.selected || st.mode !== prev.mode || st.ride !== prev.ride || st.resolvedTheme !== prev.resolvedTheme) {
        setData(map, "trails", trailsFC(st, st.resolvedTheme));
        setData(map, "via", viaFC(st));
        setData(map, "places", st.mode === "wander" && st.wander.tab === "places" ? placesFC(st.wander.result?.places ?? []) : EMPTY);
      }
      if (st.mode !== prev.mode || st.wander.radiusKm !== prev.wander.radiusKm || st.me !== prev.me || st.from !== prev.from) {
        setData(map, "radius", radiusFC(st));
      }
      if (st.meta !== prev.meta) setData(map, "region", regionFC(st));
      const vis = (id: string, on: boolean) => map.getLayer(id) && map.setLayoutProperty(id, "visibility", on ? "visible" : "none");
      const ride = st.mode === "ride";
      ["gems-halo", "gems-dot", "gems-label"].forEach((l) => vis(l, !ride));
    });
  }, []);

  // Endpoint markers.
  useEffect(() => {
    const sync = (st: State) => {
      const map = mapRef.current;
      if (!map) return;
      const o = origin(st);
      const showTrip = st.mode === "go" || st.mode === "ride";
      const set = (key: "from" | "to" | "me", at: { lat: number; lng: number } | null, make: () => HTMLElement) => {
        const cur = markers.current[key];
        if (!at) {
          cur?.remove();
          markers.current[key] = undefined;
          return;
        }
        if (cur) cur.setLngLat([at.lng, at.lat]);
        else markers.current[key] = new maplibregl.Marker({ element: make(), anchor: key === "to" ? "bottom" : "center" }).setLngLat([at.lng, at.lat]).addTo(map);
      };
      set("me", st.me && st.mode !== "ride" ? st.me : null, () => markerEl("pin-me"));
      set("from", showTrip && st.to && o && !o.isMe ? o.at : null, () => markerEl("pin-from"));
      const toKey = st.to ? `${st.to.name}` : "";
      if (markers.current.to && (markers.current.to.getElement().dataset.key ?? "") !== toKey) {
        markers.current.to.remove();
        markers.current.to = undefined;
      }
      set("to", showTrip && st.to ? st.to.at : null, () => {
        const e = markerEl("pin-to", st.to?.name);
        e.dataset.key = toKey;
        return e;
      });
    };
    const unsub = useStore.subscribe((st, prev) => {
      if (st.me !== prev.me || st.from !== prev.from || st.to !== prev.to || st.mode !== prev.mode || st.meta !== prev.meta) sync(st);
    });
    const t = setInterval(() => mapRef.current && sync(useStore.getState()), 400);
    setTimeout(() => clearInterval(t), 4000);
    return () => {
      unsub();
      clearInterval(t);
    };
  }, []);

  // Camera commands.
  const camera = useStore((s) => s.camera);
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !camera) return;
    const padding = camera.padding === "none" ? 40 : sheetPadding();
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (camera.bbox && Number.isFinite(camera.bbox[0])) {
      const b = camera.bbox;
      // flyTo leaves persistent padding behind; fitBounds would add its own on top.
      map.setPadding({ top: 0, bottom: 0, left: 0, right: 0 });
      map.fitBounds([[b[0], b[1]], [b[2], b[3]]] as LngLatBoundsLike, { padding, duration: reduce ? 0 : 1100, maxZoom: 16, essential: true });
    } else if (camera.center) {
      map.flyTo({ center: [camera.center.lng, camera.center.lat], zoom: camera.zoom ?? map.getZoom(), padding, duration: reduce ? 0 : 1200, essential: true });
    }
  }, [camera]);

  // 3D terrain.
  const terrain3d = useStore((s) => s.terrain3d);
  useEffect(() => {
    const map = mapRef.current;
    if (map?.isStyleLoaded()) applyTerrain(map, terrain3d);
  }, [terrain3d]);

  // Expose the map for the ride view (camera follow, progress line).
  useEffect(() => {
    (window as unknown as { __pdMap?: () => MLMap | null }).__pdMap = () => mapRef.current;
  }, []);

  return <div ref={el} className="map" role="region" aria-label="Map" />;
}

function applyTerrain(map: MLMap, on: boolean) {
  if (on) {
    map.setTerrain({ source: "terrain", exaggeration: 1.5 });
    map.easeTo({ pitch: 62, duration: 900 });
  } else {
    map.setTerrain(null);
    if (map.getPitch() > 0 && (window as unknown as { __pdRiding?: boolean }).__pdRiding !== true) map.easeTo({ pitch: 0, duration: 600 });
  }
}

export function getMap(): MLMap | null {
  return (window as unknown as { __pdMap?: () => MLMap | null }).__pdMap?.() ?? null;
}
