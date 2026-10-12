import type { RouteOption, RouteResponse } from "../lib/api";
import { encodePolyline } from "../lib/geo";

const line = (n: number, dLat: number, dLng: number) =>
  Array.from({ length: n }, (_, i) => ({ lat: 22.69 + i * dLat, lng: 75.85 + i * dLng }));

export const stdOption: RouteOption = {
  id: "std",
  kind: "standard",
  profile: "tw_standard",
  distance_m: 7100,
  duration_s: 1020,
  polyline: encodePolyline(line(10, -0.003, 0.003)),
  legs: [{ source: "osm", from_idx: 0, to_idx: 9 }],
  surface_breakdown: { paved_m: 7100, unpaved_m: 0, track_m: 0 },
  surface_spans: [{ from_idx: 0, to_idx: 9, kind: "paved" }],
  warnings: [],
  instructions: [
    { text: "Head south on MR 10", distance_m: 3000, duration_s: 400, sign: 0, interval: [0, 4] },
    { text: "Turn right onto Khandwa Road", distance_m: 4100, duration_s: 620, sign: 2, interval: [4, 9] },
    { text: "Arrive at destination", distance_m: 0, duration_s: 0, sign: 4, interval: [9, 9] },
  ],
};

export const scOption: RouteOption = {
  id: "sc1",
  kind: "shortcut",
  profile: "tw_shortcut",
  distance_m: 3400,
  duration_s: 600,
  polyline: encodePolyline(line(8, -0.002, 0.001)),
  legs: [
    { source: "osm", from_idx: 0, to_idx: 2 },
    { source: "community", shortcut_id: "abc", title: "Farm track", from_idx: 2, to_idx: 5 },
    { source: "osm", from_idx: 5, to_idx: 7 },
  ],
  surface_breakdown: { paved_m: 2200, unpaved_m: 1200, track_m: 0 },
  surface_spans: [
    { from_idx: 0, to_idx: 2, kind: "paved" },
    { from_idx: 2, to_idx: 5, kind: "unpaved" },
    { from_idx: 5, to_idx: 7, kind: "paved" },
  ],
  confidence: 0.82,
  last_confirmed_at: new Date(Date.now() - 3 * 86400000).toISOString(),
  confirmations_30d: 4,
  warnings: [
    { type: "unpaved", at_m: 1500, length_m: 1200, surface: "unpaved" },
    { type: "gate", at_m: 2100 },
  ],
  savings: { distance_m: 3700, duration_s: 420 },
  instructions: [
    { text: "Head south", distance_m: 500, duration_s: 60, sign: 0, interval: [0, 2] },
    { text: "Take the rider shortcut (Farm track)", distance_m: 1200, duration_s: 290, sign: 0, interval: [2, 5] },
    { text: "Turn left onto Bilawali Road", distance_m: 1700, duration_s: 250, sign: -2, interval: [5, 7] },
    { text: "Arrive at destination", distance_m: 0, duration_s: 0, sign: 4, interval: [7, 7] },
  ],
};

export const routeResponse: RouteResponse = {
  request_id: "01JTESTROUTE0000000000000",
  region: "indore",
  context: { daylight: true, season: null, vehicle: "scooter", mode: "auto", profiles: ["tw_standard", "tw_shortcut"] },
  options: [stdOption, scOption],
  recommended: "sc1",
  attribution: "© OpenStreetMap contributors",
  disclaimer: "Unverified roads can change. Ride at your own judgement.",
};
