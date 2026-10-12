export type LatLng = { lat: number; lng: number };

const R = 6371008.8;
const rad = (d: number) => (d * Math.PI) / 180;

/** Great-circle distance in metres. */
export function haversine(a: LatLng, b: LatLng): number {
  const dLat = rad(b.lat - a.lat);
  const dLng = rad(b.lng - a.lng);
  const s = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(s)));
}

/** Decode a precision-5 encoded polyline into points. */
export function decodePolyline(s: string, precision = 5): LatLng[] {
  const factor = 10 ** precision;
  const out: LatLng[] = [];
  let i = 0;
  let lat = 0;
  let lng = 0;
  const next = () => {
    let result = 0;
    let shift = 0;
    let b: number;
    do {
      if (i >= s.length) throw new Error("bad polyline");
      b = s.charCodeAt(i++) - 63;
      result |= (b & 0x1f) << shift;
      shift += 5;
    } while (b >= 0x20);
    return result & 1 ? ~(result >> 1) : result >> 1;
  };
  while (i < s.length) {
    lat += next();
    lng += next();
    out.push({ lat: lat / factor, lng: lng / factor });
  }
  return out;
}

/** Encode points as a precision-5 polyline. */
export function encodePolyline(pts: LatLng[], precision = 5): string {
  const factor = 10 ** precision;
  let out = "";
  let pLat = 0;
  let pLng = 0;
  const enc = (v: number) => {
    let u = v < 0 ? ~(v << 1) : v << 1;
    let s = "";
    while (u >= 0x20) {
      s += String.fromCharCode((0x20 | (u & 0x1f)) + 63);
      u >>= 5;
    }
    return s + String.fromCharCode(u + 63);
  };
  for (const p of pts) {
    const la = Math.round(p.lat * factor);
    const ln = Math.round(p.lng * factor);
    out += enc(la - pLat) + enc(ln - pLng);
    pLat = la;
    pLng = ln;
  }
  return out;
}

export function lineLength(pts: LatLng[]): number {
  let d = 0;
  for (let i = 1; i < pts.length; i++) d += haversine(pts[i - 1]!, pts[i]!);
  return d;
}

export function cumulative(pts: LatLng[]): number[] {
  const out = new Array<number>(pts.length).fill(0);
  for (let i = 1; i < pts.length; i++) out[i] = out[i - 1]! + haversine(pts[i - 1]!, pts[i]!);
  return out;
}

export type Projection = { point: LatLng; distM: number; alongM: number; segment: number };

/** Closest point on a line to p, with distance along the line. */
export function project(line: LatLng[], p: LatLng): Projection {
  let best: Projection = { point: line[0] ?? p, distM: Infinity, alongM: 0, segment: 0 };
  let cum = 0;
  for (let i = 0; i + 1 < line.length; i++) {
    const a = line[i]!;
    const b = line[i + 1]!;
    const k = Math.cos(rad(a.lat));
    const bx = (b.lng - a.lng) * k;
    const by = b.lat - a.lat;
    const px = (p.lng - a.lng) * k;
    const py = p.lat - a.lat;
    const l2 = bx * bx + by * by;
    const t = l2 > 0 ? Math.max(0, Math.min(1, (px * bx + py * by) / l2)) : 0;
    const q = { lat: a.lat + (b.lat - a.lat) * t, lng: a.lng + (b.lng - a.lng) * t };
    const d = haversine(q, p);
    const seg = haversine(a, b);
    if (d < best.distM) best = { point: q, distM: d, alongM: cum + seg * t, segment: i };
    cum += seg;
  }
  return best;
}

export function bounds(pts: LatLng[]): [[number, number], [number, number]] {
  let minLng = Infinity;
  let minLat = Infinity;
  let maxLng = -Infinity;
  let maxLat = -Infinity;
  for (const p of pts) {
    minLng = Math.min(minLng, p.lng);
    minLat = Math.min(minLat, p.lat);
    maxLng = Math.max(maxLng, p.lng);
    maxLat = Math.max(maxLat, p.lat);
  }
  return [
    [minLng, minLat],
    [maxLng, maxLat],
  ];
}

/** Point at a given distance along the line. */
export function pointAlong(line: LatLng[], distM: number): LatLng {
  if (line.length === 0) return { lat: 0, lng: 0 };
  let cum = 0;
  for (let i = 1; i < line.length; i++) {
    const seg = haversine(line[i - 1]!, line[i]!);
    if (cum + seg >= distM) {
      const t = seg > 0 ? (distM - cum) / seg : 0;
      const a = line[i - 1]!;
      const b = line[i]!;
      return { lat: a.lat + (b.lat - a.lat) * t, lng: a.lng + (b.lng - a.lng) * t };
    }
    cum += seg;
  }
  return line[line.length - 1]!;
}

export function parseLatLng(s: string | null): LatLng | null {
  if (!s) return null;
  const [a, b] = s.split(",").map(Number);
  if (a === undefined || b === undefined || !Number.isFinite(a) || !Number.isFinite(b)) return null;
  if (Math.abs(a) > 90 || Math.abs(b) > 180) return null;
  return { lat: a, lng: b };
}

export const fmtLatLng = (p: LatLng) => `${p.lat.toFixed(5)},${p.lng.toFixed(5)}`;
