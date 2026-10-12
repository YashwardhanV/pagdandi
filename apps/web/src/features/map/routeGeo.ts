import type { FeatureCollection, LineString, Point } from "geojson";
import type { RouteOption } from "../../lib/api";
import { decodePolyline, lineLength, pointAlong, type LatLng } from "../../lib/geo";

export type RouteProps = { option: string; kind: "standard" | "shortcut"; surface: string; selected: boolean; order: number };

/** Split each option into per-surface LineStrings so unpaved can be dashed and tracks dotted. */
export function routeFeatures(options: RouteOption[], selected: string | null, fraction = 1): FeatureCollection<LineString, RouteProps> {
  const features: FeatureCollection<LineString, RouteProps>["features"] = [];
  options.forEach((o, i) => {
    let pts: LatLng[];
    try {
      pts = decodePolyline(o.polyline);
    } catch {
      return;
    }
    const isSel = o.id === (selected ?? "");
    const limit = fraction < 1 ? Math.max(2, Math.ceil(pts.length * fraction)) : pts.length;
    const spans = o.surface_spans.length ? o.surface_spans : [{ from_idx: 0, to_idx: pts.length - 1, kind: "paved" as const }];
    for (const s of spans) {
      const end = Math.min(s.to_idx, limit - 1);
      if (end <= s.from_idx) continue;
      const seg = pts.slice(s.from_idx, end + 1);
      if (seg.length < 2) continue;
      features.push({
        type: "Feature",
        properties: { option: o.id, kind: o.kind, surface: s.kind, selected: isSel, order: isSel ? 100 : i },
        geometry: { type: "LineString", coordinates: seg.map((p) => [p.lng, p.lat]) },
      });
    }
  });
  features.sort((a, b) => a.properties.order - b.properties.order);
  return { type: "FeatureCollection", features };
}

/** Midpoint labels for community legs ("Rider shortcut"). */
export function communityLabels(options: RouteOption[], selected: string | null, label: string): FeatureCollection<Point, { label: string }> {
  const out: FeatureCollection<Point, { label: string }> = { type: "FeatureCollection", features: [] };
  for (const o of options) {
    if (selected && o.id !== selected) continue;
    let pts: LatLng[];
    try {
      pts = decodePolyline(o.polyline);
    } catch {
      continue;
    }
    for (const leg of o.legs) {
      if (leg.source !== "community") continue;
      const seg = pts.slice(leg.from_idx, leg.to_idx + 1);
      const mid = pointAlong(seg, lineLength(seg) / 2);
      out.features.push({ type: "Feature", properties: { label }, geometry: { type: "Point", coordinates: [mid.lng, mid.lat] } });
    }
  }
  return out;
}

export function lineFeature(pts: LatLng[]): FeatureCollection<LineString> {
  return {
    type: "FeatureCollection",
    features: pts.length > 1 ? [{ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: pts.map((p) => [p.lng, p.lat]) } }] : [],
  };
}
