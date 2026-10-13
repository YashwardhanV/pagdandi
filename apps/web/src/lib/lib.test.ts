import { describe, expect, it } from "vitest";
import type { Trail } from "./api";
import { deltaDist, deltaTime, duration, km, monthLabel } from "./format";
import { circle, cumulative, haversine, pointAlong, project } from "./geo";
import { aheadNote, prepare, stateAt } from "./nav";
import { isDark, sunTimes } from "./sun";

const INDORE = { lat: 22.7196, lng: 75.8577 };

describe("sun", () => {
  it("puts Indore's October sunset around 18:00 IST", () => {
    const t = sunTimes(new Date("2026-10-13T07:00:00Z"), INDORE.lat, INDORE.lng)!;
    const ist = (d: Date) => (d.getUTCHours() + 5.5 + d.getUTCMinutes() / 60) % 24;
    expect(ist(t.rise)).toBeGreaterThan(6);
    expect(ist(t.rise)).toBeLessThan(6.7);
    expect(ist(t.set)).toBeGreaterThan(17.7);
    expect(ist(t.set)).toBeLessThan(18.3);
  });
  it("is light at noon and dark at 21:00 IST", () => {
    expect(isDark(new Date("2026-10-13T06:30:00Z"), INDORE.lat, INDORE.lng)).toBe(false);
    expect(isDark(new Date("2026-10-13T15:30:00Z"), INDORE.lat, INDORE.lng)).toBe(true);
  });
});

describe("geo", () => {
  it("measures and walks a line", () => {
    const line: [number, number][] = [
      [75.85, 22.71],
      [75.86, 22.71],
      [75.86, 22.72],
    ];
    const cum = cumulative(line);
    expect(cum[2]).toBeGreaterThan(2000);
    const mid = pointAlong(line, cum, cum[1] / 2);
    expect(mid.p.lng).toBeCloseTo(75.855, 3);
    expect(Math.round(mid.heading)).toBe(90);
    const pr = project(line, cum, { lat: 22.7105, lng: 75.858 });
    expect(pr.distM).toBeLessThan(80);
    expect(pr.alongM).toBeGreaterThan(700);
  });
  it("draws a closed circle of the right size", () => {
    const ring = circle(INDORE, 5000, 32);
    expect(ring[0]).toEqual(ring[ring.length - 1]);
    expect(haversine(INDORE, { lng: ring[8][0], lat: ring[8][1] })).toBeCloseTo(5000, -1);
  });
});

describe("format", () => {
  it("reads like a person", () => {
    expect(km(420)).toBe("420 m");
    expect(km(12345)).toBe("12 km");
    expect(km(3400)).toBe("3.4 km");
    expect(duration(3720)).toBe("1 h 2 min");
    expect(deltaTime(-180)).toBe("−3 min");
    expect(deltaDist(30)).toBe("same distance");
    expect(monthLabel("2026-03")).toBe("Mar 2026");
  });
});

function trail(): Trail {
  const geometry: [number, number][] = Array.from({ length: 11 }, (_, i) => [75.85 + i * 0.001, 22.71]);
  const total = cumulative(geometry)[10];
  return {
    id: "t",
    kind: "kaccha",
    name: "Kaccha",
    name_hi: "",
    tagline: "",
    distance_m: total,
    duration_s: 600,
    delta_m: 0,
    delta_s: 0,
    climb_m: 0,
    descent_m: 0,
    geometry,
    elevation: null,
    surfaces: [
      { kind: "paved", from_m: 0, to_m: total / 2 },
      { kind: "dirt", from_m: total / 2, to_m: total },
    ],
    mix: { paved: 0.5, dirt: 0.5 },
    quiet: 0.5,
    scenic: 0.5,
    twisty: 0,
    backroads_m: 0,
    fresh_m: 0,
    steps: [
      { text: "Turn left onto the track", sign: -2, distance_m: 0, duration_s: 0, at: geometry[5], index: 5, along_m: total / 2 },
      { text: "Arrive", sign: 4, distance_m: 0, duration_s: 0, at: geometry[10], index: 10, along_m: total },
    ],
    bbox: [75.85, 22.71, 75.86, 22.71],
  };
}

describe("nav", () => {
  it("knows the next turn, what's ahead and when you've arrived", () => {
    const p = prepare(trail());
    const s = stateAt(p, 100);
    expect(s.step?.text).toContain("Turn left");
    expect(s.toStepM).toBeGreaterThan(300);
    expect(aheadNote(p.trail, 100)).toMatch(/^Kaccha starts in/);
    expect(stateAt(p, p.total - 10).arrived).toBe(true);
    expect(Math.round(stateAt(p, p.total / 2).remainingS)).toBe(300);
  });
});
