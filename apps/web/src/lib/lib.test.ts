import { describe, expect, it } from "vitest";
import { formatAgo, formatDistance, formatDuration, minutes } from "./format";
import { decodePolyline, encodePolyline, haversine, parseLatLng, pointAlong, project } from "./geo";
import { translate } from "./i18n";
import { isDaylight, sunTimes } from "./time";
import en from "../i18n/en.json";
import hi from "../i18n/hi.json";
import sv from "../i18n/sv.json";

describe("format", () => {
  it("always shows units", () => {
    expect(formatDistance(850)).toBe("850 m");
    expect(formatDistance(42)).toBe("40 m");
    expect(formatDistance(7100)).toBe("7.1 km");
    expect(formatDistance(24300)).toBe("24 km");
    expect(formatDuration(1020)).toBe("17 min");
    expect(formatDuration(20)).toBe("1 min");
    expect(formatDuration(3900)).toBe("1 h 05 min");
    expect(minutes(420)).toBe(7);
    expect(formatDistance(1200, "hi")).toBe("1.2 किमी");
  });
  it("formats relative time", () => {
    const now = Date.parse("2026-10-12T12:00:00Z");
    expect(formatAgo("2026-10-09T12:00:00Z", "en", now)).toBe("3 days ago");
    expect(formatAgo(null)).toBe("");
  });
});

describe("geo", () => {
  it("round-trips polylines", () => {
    const pts = [{ lat: 38.5, lng: -120.2 }, { lat: 40.7, lng: -120.95 }, { lat: 43.252, lng: -126.453 }];
    const enc = encodePolyline(pts);
    expect(enc).toBe("_p~iF~ps|U_ulLnnqC_mqNvxq`@");
    decodePolyline(enc).forEach((p, i) => {
      expect(p.lat).toBeCloseTo(pts[i]!.lat, 5);
      expect(p.lng).toBeCloseTo(pts[i]!.lng, 5);
    });
  });
  it("projects points and walks along lines", () => {
    const line = [{ lat: 0, lng: 0 }, { lat: 0, lng: 0.01 }];
    const p = project(line, { lat: 0.001, lng: 0.005 });
    expect(p.distM).toBeCloseTo(111.2, 0);
    expect(p.alongM).toBeCloseTo(556, -1);
    expect(haversine(line[0]!, pointAlong(line, 500))).toBeCloseTo(500, 0);
  });
  it("parses lat,lng safely", () => {
    expect(parseLatLng("22.7,75.8")).toEqual({ lat: 22.7, lng: 75.8 });
    expect(parseLatLng("abc")).toBeNull();
    expect(parseLatLng("95,10")).toBeNull();
  });
});

describe("i18n", () => {
  it("fills placeholders and falls back to English", () => {
    expect(translate("en", "route.saves_min", { n: 9 })).toBe("saves 9 min");
    expect(translate("hi", "route.saves_min", { n: 9 })).toBe("9 मिनट बचत");
    expect(translate("sv", "surface.unpaved.SE")).toBe("Grusväg");
  });
  it("has every English key in Hindi and Swedish", () => {
    const keys = Object.keys(en);
    expect(keys.filter((k) => !(k in hi))).toEqual([]);
    expect(keys.filter((k) => !(k in sv))).toEqual([]);
  });
  it("keeps chip labels short enough not to overflow (SPEC §11.7)", () => {
    const chipKeys = ["warn.gate", "warn.night", "warn.steep", "warn.low_confidence", "warn.floods", "surface.unpaved.IN", "surface.unpaved.SE", "route.recommended", "mode.adventure"];
    for (const d of [en, hi, sv] as Record<string, string>[]) for (const k of chipKeys) expect(d[k]!.length).toBeLessThanOrEqual(24);
  });
});

describe("time", () => {
  it("finds sunrise/sunset and daylight in Indore", () => {
    const t = sunTimes(new Date("2026-06-21T06:00:00Z"), 22.7196, 75.8577)!;
    // 05:41 and 19:13 IST
    expect(Math.abs(t.sunrise.getTime() - Date.parse("2026-06-21T00:11:00Z"))).toBeLessThan(5 * 60000);
    expect(Math.abs(t.sunset.getTime() - Date.parse("2026-06-21T13:43:00Z"))).toBeLessThan(5 * 60000);
    expect(isDaylight(new Date("2026-10-12T07:30:00Z"), 22.72, 75.86)).toBe(true);
    expect(isDaylight(new Date("2026-10-12T17:30:00Z"), 22.72, 75.86)).toBe(false);
  });
});
