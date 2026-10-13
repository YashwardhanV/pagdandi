import type { LatLng } from "./api";

const R = 6371008.8;
const rad = (d: number) => (d * Math.PI) / 180;

export function haversine(a: LatLng, b: LatLng): number {
  const dLat = rad(b.lat - a.lat);
  const dLng = rad(b.lng - a.lng);
  const s = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(s)));
}

export function destination(p: LatLng, bearingDeg: number, distM: number): LatLng {
  const d = distM / R;
  const t = rad(bearingDeg);
  const f1 = rad(p.lat);
  const l1 = rad(p.lng);
  const f2 = Math.asin(Math.sin(f1) * Math.cos(d) + Math.cos(f1) * Math.sin(d) * Math.cos(t));
  const l2 = l1 + Math.atan2(Math.sin(t) * Math.sin(d) * Math.cos(f1), Math.cos(d) - Math.sin(f1) * Math.sin(f2));
  return { lat: (f2 * 180) / Math.PI, lng: ((((l2 * 180) / Math.PI + 540) % 360) - 180) };
}

export function bearing(a: LatLng, b: LatLng): number {
  const f1 = rad(a.lat);
  const f2 = rad(b.lat);
  const dl = rad(b.lng - a.lng);
  const y = Math.sin(dl) * Math.cos(f2);
  const x = Math.cos(f1) * Math.sin(f2) - Math.sin(f1) * Math.cos(f2) * Math.cos(dl);
  return ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360;
}

/** Closed [lng, lat] ring approximating a circle. */
export function circle(c: LatLng, radiusM: number, n = 96): [number, number][] {
  const ring: [number, number][] = [];
  for (let i = 0; i < n; i++) {
    const p = destination(c, (i * 360) / n, radiusM);
    ring.push([p.lng, p.lat]);
  }
  ring.push(ring[0]);
  return ring;
}

export function cumulative(line: [number, number][]): number[] {
  const out = new Array<number>(line.length).fill(0);
  for (let i = 1; i < line.length; i++) {
    out[i] = out[i - 1] + haversine({ lng: line[i - 1][0], lat: line[i - 1][1] }, { lng: line[i][0], lat: line[i][1] });
  }
  return out;
}

/** Point at distance d along a line (with its segment index and heading). */
export function pointAlong(line: [number, number][], cum: number[], d: number): { p: LatLng; seg: number; heading: number } {
  if (line.length === 1) return { p: { lng: line[0][0], lat: line[0][1] }, seg: 0, heading: 0 };
  let lo = 1;
  let hi = cum.length - 1;
  if (d <= 0) lo = 1;
  else if (d >= cum[hi]) lo = hi;
  else {
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (cum[mid] < d) lo = mid + 1;
      else hi = mid;
    }
  }
  const i = lo;
  const seg = cum[i] - cum[i - 1];
  const t = seg > 0 ? Math.min(1, Math.max(0, (d - cum[i - 1]) / seg)) : 0;
  const a = { lng: line[i - 1][0], lat: line[i - 1][1] };
  const b = { lng: line[i][0], lat: line[i][1] };
  return { p: { lat: a.lat + (b.lat - a.lat) * t, lng: a.lng + (b.lng - a.lng) * t }, seg: i - 1, heading: bearing(a, b) };
}

/** Projects p onto the line: distance from it and distance along it. */
export function project(line: [number, number][], cum: number[], p: LatLng): { distM: number; alongM: number } {
  let best = { distM: Infinity, alongM: 0 };
  const kx = 111320 * Math.cos(rad(p.lat));
  const ky = 110540;
  for (let i = 0; i + 1 < line.length; i++) {
    const ax = (line[i][0] - p.lng) * kx;
    const ay = (line[i][1] - p.lat) * ky;
    const bx = (line[i + 1][0] - p.lng) * kx;
    const by = (line[i + 1][1] - p.lat) * ky;
    const dx = bx - ax;
    const dy = by - ay;
    const len2 = dx * dx + dy * dy;
    let t = len2 > 0 ? -(ax * dx + ay * dy) / len2 : 0;
    t = Math.max(0, Math.min(1, t));
    const qx = ax + dx * t;
    const qy = ay + dy * t;
    const d = Math.hypot(qx, qy);
    if (d < best.distM) best = { distM: d, alongM: cum[i] + (cum[i + 1] - cum[i]) * t };
  }
  return best;
}

export function sliceLine(line: [number, number][], cum: number[], from: number, to: number): [number, number][] {
  if (to <= from) return [];
  const a = pointAlong(line, cum, from);
  const b = pointAlong(line, cum, to);
  const out: [number, number][] = [[a.p.lng, a.p.lat]];
  for (let i = a.seg + 1; i <= b.seg; i++) out.push(line[i]);
  out.push([b.p.lng, b.p.lat]);
  return out;
}

export function bboxOf(points: [number, number][]): [number, number, number, number] {
  let b: [number, number, number, number] = [Infinity, Infinity, -Infinity, -Infinity];
  for (const [x, y] of points) b = [Math.min(b[0], x), Math.min(b[1], y), Math.max(b[2], x), Math.max(b[3], y)];
  return b;
}
