import type { Page, Route } from "@playwright/test";
import { encodePolyline } from "../src/lib/geo";

export const ORIGIN = { lat: 22.6893, lng: 75.8529 };
export const DEST = { lat: 22.6549, lng: 75.8722 };

const lerp = (a: typeof ORIGIN, b: typeof ORIGIN, n: number) =>
  Array.from({ length: n + 1 }, (_, i) => ({ lat: a.lat + ((b.lat - a.lat) * i) / n, lng: a.lng + ((b.lng - a.lng) * i) / n }));

const via = { lat: 22.7005, lng: 75.8795 };
const stdLine = [...lerp(ORIGIN, via, 6), ...lerp(via, DEST, 6).slice(1)];
const scLine = lerp(ORIGIN, DEST, 10);

export const region = {
  id: "indore", name: "Indore", country: "IN", timezone: "Asia/Kolkata", center: [22.7196, 75.8577], radius_km: 150,
  bbox: [74.4, 21.4, 77.3, 24.1], langs: ["en", "hi"], default_vehicle: "scooter", vehicles: ["scooter", "motorcycle"],
  seasons: [{ name: "monsoon", from: "06-01", to: "09-30", unpaved_penalty: 1.6, confidence_half_life_days: 7 }],
  active_season: null, seeded: true,
};

export const routeResponse = {
  request_id: "01JE2EROUTE00000000000000",
  region: "indore",
  context: { daylight: true, season: null, vehicle: "scooter", mode: "auto", profiles: ["tw_standard", "tw_shortcut"] },
  recommended: "sc1",
  attribution: "© OpenStreetMap contributors",
  disclaimer: "Unverified roads can change. Ride at your own judgement.",
  options: [
    {
      id: "std", kind: "standard", profile: "tw_standard", distance_m: 7100, duration_s: 1020,
      polyline: encodePolyline(stdLine), legs: [{ source: "osm", from_idx: 0, to_idx: stdLine.length - 1 }],
      surface_breakdown: { paved_m: 7100, unpaved_m: 0, track_m: 0 },
      surface_spans: [{ from_idx: 0, to_idx: stdLine.length - 1, kind: "paved" }], warnings: [],
      instructions: [
        { text: "Head east on MR 10", distance_m: 3500, duration_s: 500, sign: 0, interval: [0, 6] },
        { text: "Turn right at Khandwa Naka", distance_m: 3600, duration_s: 520, sign: 2, interval: [6, 12] },
        { text: "Arrive at destination", distance_m: 0, duration_s: 0, sign: 4, interval: [12, 12] },
      ],
    },
    {
      id: "sc1", kind: "shortcut", profile: "tw_shortcut", distance_m: 3400, duration_s: 600,
      polyline: encodePolyline(scLine),
      legs: [
        { source: "osm", from_idx: 0, to_idx: 3 },
        { source: "community", shortcut_id: "sc-demo", title: "Farm track", from_idx: 3, to_idx: 7 },
        { source: "osm", from_idx: 7, to_idx: 10 },
      ],
      surface_breakdown: { paved_m: 2200, unpaved_m: 1200, track_m: 0 },
      surface_spans: [
        { from_idx: 0, to_idx: 3, kind: "paved" },
        { from_idx: 3, to_idx: 7, kind: "unpaved" },
        { from_idx: 7, to_idx: 10, kind: "paved" },
      ],
      confidence: 0.82, last_confirmed_at: new Date(Date.now() - 3 * 86400000).toISOString(), confirmations_30d: 4,
      warnings: [{ type: "unpaved", at_m: 1500, length_m: 1200, surface: "unpaved" }, { type: "gate", at_m: 2100 }],
      savings: { distance_m: 3700, duration_s: 420 },
      instructions: [
        { text: "Head south", distance_m: 1000, duration_s: 150, sign: 0, interval: [0, 3] },
        { text: "Take the rider shortcut (Farm track)", distance_m: 1200, duration_s: 290, sign: 0, interval: [3, 7] },
        { text: "Turn left towards Bilawali", distance_m: 1200, duration_s: 160, sign: -2, interval: [7, 10] },
        { text: "Arrive at destination", distance_m: 0, duration_s: 0, sign: 4, interval: [10, 10] },
      ],
    },
  ],
};

export const shortcutDetail = {
  shortcut: {
    id: "sc-new", region_id: "indore", title: "Behind the mandi", polyline: encodePolyline(lerp(ORIGIN, { lat: 22.684, lng: 75.857 }, 4)),
    length_m: 700, oneway: false, surface: "unpaved", vehicles: ["scooter", "motorcycle"], flags: ["gate"], status: "pending",
    confidence: 0.5, confidence_band: "medium", last_confirmed_at: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
  },
  reports: [], history: [{ day: "2026-10-12", confidence: 0.5 }, { day: "2026-10-13", confidence: 0.5 }],
  confirmations_30d: 0, report_count: 0, mine: true, osm_edit_url: "https://www.openstreetmap.org/edit#map=18/22.68/75.85",
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

export type MockState = { reports: unknown[]; created: unknown[]; signedIn: boolean };

/** Mock every API and map request so tests are deterministic and offline-safe. */
export async function mockAll(page: Page, opts: { signedIn?: boolean } = {}): Promise<MockState> {
  const state: MockState = { reports: [], created: [], signedIn: !!opts.signedIn };
  // Map: no local tiles; the remote style is replaced with a plain background.
  await page.route("**/tiles/manifest.json", (r) => r.fulfill({ status: 404 }));
  await page.route("https://tiles.openfreemap.org/**", (r) =>
    json(r, { version: 8, sources: {}, layers: [{ id: "bg", type: "background", paint: { "background-color": "#F2F1EE" } }] }),
  );
  await page.route("https://protomaps.github.io/**", (r) => r.fulfill({ status: 404 }));
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname;
    const m = route.request().method();
    if (p === "/v1/regions") return json(route, { regions: [region] });
    if (p === "/v1/regions/indore/destinations")
      return json(route, {
        region: "indore",
        destinations: [{ id: "d1", region_id: "indore", name: "Bilawali Lake", category: "lake", lat: DEST.lat, lng: DEST.lng, approx_km: 8, verified: true, nearby_shortcuts: 2 }],
      });
    if (p === "/v1/geocode")
      return json(route, {
        results: [{ id: "osm:w1", name: "Bilawali Lake View", label: "Indore, Madhya Pradesh", lat: DEST.lat, lng: DEST.lng, source: "photon", in_coverage: true, region_id: "indore" }],
      });
    if (p === "/v1/route") return json(route, routeResponse);
    if (p === "/v1/shortcuts" && m === "GET") return json(route, { shortcuts: [] });
    if (p === "/v1/shortcuts/snap") {
      const b = route.request().postDataJSON() as { lat: number; lng: number };
      return json(route, { ok: true, node_id: 42, lat: b.lat, lng: b.lng, distance_m: 4 });
    }
    if (p === "/v1/shortcuts" && m === "POST") {
      state.created.push(route.request().postDataJSON());
      return json(route, { shortcut: shortcutDetail.shortcut, message: "ok" }, 201);
    }
    if (/^\/v1\/shortcuts\/[^/]+\/reports$/.test(p)) {
      state.reports.push(route.request().postDataJSON());
      return json(route, { confidence: 0.85, band: "high", status: "active" }, 201);
    }
    if (/^\/v1\/shortcuts\/[^/]+$/.test(p)) return json(route, shortcutDetail);
    if (p === "/v1/me")
      return state.signedIn
        ? json(route, { user: { id: "u1", email: "rider@example.com", display_name: "Asha", home_region: "indore", trust: 1, role: "rider", traces_consent_at: null, created_at: new Date().toISOString() }, trusted: false })
        : json(route, { error: { code: "UNAUTHENTICATED", message: "Please sign in first." } }, 401);
    if (p === "/v1/me/contributions") return json(route, { shortcuts: [], reports: 0, active_shortcuts: 0 });
    if (p === "/v1/auth/providers") return json(route, { email: true, google: false });
    if (p === "/v1/auth/magic-link") return json(route, { sent: true }, 202);
    return json(route, { error: { code: "NOT_FOUND", message: "No such endpoint." } }, 404);
  });
  return state;
}
