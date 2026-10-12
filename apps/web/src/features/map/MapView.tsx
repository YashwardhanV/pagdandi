import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import Map, { AttributionControl, Layer, Marker, Source, type MapRef } from "react-map-gl/maplibre";
import maplibregl, { type StyleSpecification } from "maplibre-gl";
import { Protocol } from "pmtiles";
import { useQuery } from "@tanstack/react-query";
import "maplibre-gl/dist/maplibre-gl.css";

import { api } from "../../lib/api";
import { bounds, decodePolyline } from "../../lib/geo";
import { useDesktop, useOnline, prefersReducedMotion } from "../../lib/hooks";
import { useT } from "../../lib/i18n";
import { regionCenter, useRegion, useTilesManifest } from "../../lib/queries";
import { useScheme } from "../../lib/theme";
import { useApp } from "../../state/store";
import { useMapStore } from "./mapStore";
import { communityLabels, lineFeature, routeFeatures } from "./routeGeo";
import { PALETTE, protomapsStyle, remoteStyle } from "./style";

let protocolAdded = false;
function ensurePMTiles() {
  if (protocolAdded) return;
  maplibregl.addProtocol("pmtiles", new Protocol().tile);
  protocolAdded = true;
}

const tilesMode = (import.meta.env.VITE_TILES_MODE as string | undefined) ?? "local";

function useMapStyle(scheme: "light" | "dark", country: string, regionId: string | undefined) {
  const manifest = useTilesManifest();
  const online = useOnline();
  const key = country.toLowerCase();
  const pack = regionId ? manifest.data?.packs?.[regionId] : undefined;
  // Offline: use the downloaded pack for the region (served by the service worker cache).
  const offlinePack = useQuery({
    queryKey: ["pack-cached", regionId, online],
    enabled: !online && !!pack && "caches" in window,
    queryFn: async () => !!(await (await caches.open("pd-offline-packs")).match(`/tiles/${pack!.file}`)),
  });
  let local = tilesMode !== "remote" && manifest.data && manifest.data[key] ? `${location.origin}/tiles/${key}.pmtiles` : null;
  if (!online && pack && offlinePack.data) local = `${location.origin}/tiles/${pack.file}`;
  return useQuery({
    queryKey: ["map-style", scheme, country, local],
    enabled: !manifest.isLoading,
    staleTime: Infinity,
    queryFn: async (): Promise<StyleSpecification> => (local ? protomapsStyle(local, scheme, country) : remoteStyle(scheme)),
  });
}

export default function MapView() {
  ensurePMTiles();
  const t = useT();
  const navigate = useNavigate();
  const desktop = useDesktop();
  const scheme = useScheme();
  const colors = PALETTE[scheme];
  const region = useRegion();
  const country = region?.country ?? "IN";
  const style = useMapStyle(scheme, country, region?.id);
  const ref = useRef<MapRef>(null);

  const route = useApp((s) => s.route);
  const selected = useApp((s) => s.selected);
  const setApp = useApp((s) => s.set);
  const userPos = useApp((s) => s.userPos);
  const { highlight, draft, onClick, fit, follow, set: setMap } = useMapStore();

  const [view, setView] = useState<{ bbox: [number, number, number, number]; zoom: number } | null>(null);

  // Draw-on animation: 300 ms, once per route load (SPEC §11.5).
  const [progress, setProgress] = useState(1);
  useEffect(() => {
    if (!route) return;
    let raf = 0;
    if (prefersReducedMotion()) {
      raf = requestAnimationFrame(() => setProgress(1));
      return () => cancelAnimationFrame(raf);
    }
    const start = performance.now();
    const step = (now: number) => {
      const p = Math.min(1, (now - start) / 300);
      setProgress(1 - (1 - p) ** 3);
      if (p < 1) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [route]);

  const routeData = useMemo(() => (route ? routeFeatures(route.options, selected ?? route.recommended, progress) : null), [route, selected, progress]);
  const labelData = useMemo(
    () => (route ? communityLabels(route.options, selected ?? route.recommended, t("route.rider_shortcut")) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [route, selected],
  );

  // Fit the map to a new route, leaving room for the sheet or side panel.
  useEffect(() => {
    if (!route || !ref.current) return;
    const pts = route.options.flatMap((o) => {
      try {
        return decodePolyline(o.polyline);
      } catch {
        return [];
      }
    });
    if (pts.length < 2) return;
    const padding = desktop ? { top: 64, bottom: 64, left: 440, right: 64 } : { top: 120, bottom: window.innerHeight * 0.55, left: 32, right: 32 };
    ref.current.fitBounds(bounds(pts), { padding, duration: prefersReducedMotion() ? 0 : 600 });
  }, [route, desktop]);

  useEffect(() => {
    if (!fit || !ref.current) return;
    const p = fit.padding ?? 60;
    const padding = desktop ? { top: p, bottom: p, left: 400 + p, right: p } : { top: p + 60, bottom: window.innerHeight * 0.5, left: p, right: p };
    ref.current.fitBounds(fit.bounds, { padding, duration: prefersReducedMotion() ? 0 : 500, maxZoom: 17 });
  }, [fit, desktop]);

  useEffect(() => {
    if (follow && userPos && ref.current) {
      ref.current.easeTo({ center: [userPos.lng, userPos.lat], zoom: Math.max(ref.current.getZoom(), 16), bearing: userPos.heading ?? undefined, duration: 600 });
    }
  }, [follow, userPos]);

  // Community shortcuts in view.
  const shortcuts = useQuery({
    queryKey: ["shortcuts-view", view?.bbox.map((v) => v.toFixed(2)).join(","), Math.round(view?.zoom ?? 0)],
    enabled: !!view && view.zoom >= 11,
    queryFn: () => api.shortcuts(view!.bbox, view!.zoom),
    staleTime: 60_000,
    placeholderData: (prev) => prev,
  });
  const shortcutData = useMemo(() => {
    const fc: GeoJSON.FeatureCollection<GeoJSON.LineString, { id: string; status: string; band: string; surface: string }> = { type: "FeatureCollection", features: [] };
    for (const s of shortcuts.data?.shortcuts ?? []) {
      try {
        fc.features.push({
          type: "Feature",
          properties: { id: s.id, status: s.status, band: s.confidence_band, surface: s.surface },
          geometry: { type: "LineString", coordinates: decodePolyline(s.polyline).map((p) => [p.lng, p.lat]) },
        });
      } catch {
        /* skip malformed */
      }
    }
    return fc;
  }, [shortcuts.data]);

  const highlightData = useMemo(() => {
    if (!highlight) return null;
    try {
      return lineFeature(decodePolyline(highlight.polyline));
    } catch {
      return null;
    }
  }, [highlight]);

  const draftPoints = useMemo(() => {
    const fc: GeoJSON.FeatureCollection<GeoJSON.Point, { state: string }> = { type: "FeatureCollection", features: [] };
    draft?.points.forEach((p, i) => {
      const end = i === 0 || i === draft.points.length - 1;
      const snap = draft.snaps[i];
      fc.features.push({
        type: "Feature",
        properties: { state: !end ? "mid" : snap == null ? "pending" : snap.ok ? "ok" : "far" },
        geometry: { type: "Point", coordinates: [p.lng, p.lat] },
      });
    });
    return fc;
  }, [draft]);

  const updateView = () => {
    const m = ref.current;
    if (!m) return;
    const b = m.getBounds();
    setView({ bbox: [b.getWest(), b.getSouth(), b.getEast(), b.getNorth()], zoom: m.getZoom() });
  };

  if (!region || style.isLoading || !style.data) {
    return <div className="absolute inset-0 bg-bg" aria-hidden />;
  }

  const lineColor = ["case", ["==", ["get", "kind"], "standard"], colors.standard, colors.accent];
  const width = ["case", ["==", ["get", "kind"], "standard"], 6, 7];
  const selOpacity = ["case", ["get", "selected"], 1, 0.45];

  return (
    <Map
      ref={ref}
      mapLib={maplibregl}
      mapStyle={style.data}
      initialViewState={{ latitude: regionCenter(region).lat, longitude: regionCenter(region).lng, zoom: 12 }}
      style={{ position: "absolute", inset: 0 }}
      attributionControl={false}
      dragRotate={false}
      maxPitch={0}
      onLoad={(e) => {
        setMap({ map: e.target });
        updateView();
      }}
      onMoveEnd={updateView}
      interactiveLayerIds={["shortcuts-hit", "routes-hit"]}
      onClick={(e) => {
        if (onClick) return onClick({ lat: e.lngLat.lat, lng: e.lngLat.lng }, e as unknown as maplibregl.MapMouseEvent);
        const f = e.features?.[0];
        if (!f) return;
        if (f.layer.id === "routes-hit") setApp({ selected: String(f.properties?.option) });
        else if (f.layer.id === "shortcuts-hit") navigate(`/s/${f.properties?.id}`);
      }}
      cursor={onClick ? "crosshair" : undefined}
    >
      <AttributionControl compact={false} position={desktop ? "bottom-right" : "top-right"} customAttribution="© OpenStreetMap contributors" />

      <Source id="shortcuts" type="geojson" data={shortcutData}>
        <Layer id="shortcuts-casing" type="line" minzoom={11} layout={{ "line-cap": "round", "line-join": "round" }} paint={{ "line-color": colors.casing, "line-width": 6 }} />
        <Layer
          id="shortcuts-line"
          type="line"
          minzoom={11}
          layout={{ "line-cap": "round", "line-join": "round" }}
          paint={{
            "line-color": ["case", ["==", ["get", "status"], "active"], colors.accent, colors.pending] as never,
            "line-width": 3,
            "line-opacity": ["case", ["==", ["get", "band"], "low"], 0.5, 0.85] as never,
            "line-dasharray": [2, 1.5],
          }}
        />
        <Layer id="shortcuts-hit" type="line" minzoom={11} paint={{ "line-color": colors.accent, "line-width": 20, "line-opacity": 0 }} />
      </Source>

      {routeData && (
        <Source id="routes" type="geojson" data={routeData}>
          <Layer id="routes-casing" type="line" layout={{ "line-cap": "round", "line-join": "round" }} paint={{ "line-color": colors.casing, "line-width": ["+", width, 4] as never, "line-opacity": selOpacity as never }} />
          <Layer
            id="routes-paved"
            type="line"
            filter={["==", ["get", "surface"], "paved"]}
            layout={{ "line-cap": "round", "line-join": "round" }}
            paint={{ "line-color": lineColor as never, "line-width": width as never, "line-opacity": selOpacity as never }}
          />
          <Layer
            id="routes-unpaved"
            type="line"
            filter={["==", ["get", "surface"], "unpaved"]}
            layout={{ "line-join": "round" }}
            paint={{ "line-color": lineColor as never, "line-width": width as never, "line-opacity": selOpacity as never, "line-dasharray": [1.6, 1] }}
          />
          <Layer
            id="routes-track"
            type="line"
            filter={["==", ["get", "surface"], "track"]}
            layout={{ "line-cap": "round", "line-join": "round" }}
            paint={{ "line-color": lineColor as never, "line-width": width as never, "line-opacity": selOpacity as never, "line-dasharray": [0.1, 1.6] }}
          />
          <Layer id="routes-hit" type="line" paint={{ "line-color": colors.accent, "line-width": 22, "line-opacity": 0 }} />
        </Source>
      )}
      {labelData && (
        <Source id="community-labels" type="geojson" data={labelData}>
          <Layer
            id="community-labels"
            type="symbol"
            layout={{ "text-field": ["get", "label"], "text-font": ["Noto Sans Medium"], "text-size": 12, "text-offset": [0, -1.4], "text-allow-overlap": true }}
            paint={{ "text-color": colors.accent, "text-halo-color": colors.halo, "text-halo-width": 2 }}
          />
        </Source>
      )}

      {highlightData && (
        <Source id="highlight" type="geojson" data={highlightData}>
          <Layer id="highlight-casing" type="line" layout={{ "line-cap": "round" }} paint={{ "line-color": colors.casing, "line-width": 11 }} />
          <Layer
            id="highlight-line"
            type="line"
            layout={{ "line-cap": "round", "line-join": "round" }}
            paint={{ "line-color": highlight?.kind === "candidate" ? colors.warn : colors.accent, "line-width": 7, "line-dasharray": [1.6, 1] }}
          />
        </Source>
      )}

      {draft && draft.points.length > 0 && (
        <>
          <Source id="draft-line" type="geojson" data={lineFeature(draft.points)}>
            <Layer id="draft-line" type="line" layout={{ "line-cap": "round", "line-join": "round" }} paint={{ "line-color": colors.accent, "line-width": 5 }} />
          </Source>
          <Source id="draft-points" type="geojson" data={draftPoints}>
            <Layer
              id="draft-points"
              type="circle"
              paint={{
                "circle-radius": ["case", ["==", ["get", "state"], "mid"], 5, 9] as never,
                "circle-color": ["match", ["get", "state"], "ok", colors.ok, "far", colors.danger, "pending", colors.pending, colors.casing] as never,
                "circle-stroke-color": ["match", ["get", "state"], "mid", colors.accent, colors.casing] as never,
                "circle-stroke-width": 3,
              }}
            />
          </Source>
        </>
      )}

      {userPos && (
        <Marker longitude={userPos.lng} latitude={userPos.lat} anchor="center">
          <span
            className="block h-5 w-5 rounded-full border-[3px] border-surface bg-route-standard shadow-float"
            role="img"
            aria-label="Your location"
          />
        </Marker>
      )}
    </Map>
  );
}
