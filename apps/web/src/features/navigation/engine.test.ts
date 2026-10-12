import { describe, expect, it } from "vitest";
import { decodePolyline, pointAlong } from "../../lib/geo";
import { scOption } from "../../test/fixtures";
import { trimFixes } from "../shortcuts/AddShortcut";
import { arrived, communityShortcut, nextManeuver, OffRouteTracker, prepare, progress, surfaceAhead } from "./engine";

describe("navigation engine", () => {
  const r = prepare(scOption);
  const line = decodePolyline(scOption.polyline);

  it("tracks progress along the route", () => {
    const p = progress(r, pointAlong(line, 300));
    expect(p.alongM).toBeCloseTo(300, -1);
    expect(p.offM).toBeLessThan(1);
    expect(p.remainingM).toBeCloseTo(r.total - 300, -1);
  });

  it("finds the next maneuver ahead", () => {
    const m = nextManeuver(r, 100)!;
    expect(m.instruction.text).toMatch(/rider shortcut/);
    expect(m.distanceM).toBeCloseTo((r.cum[2] ?? 0) - 100, 0);
    expect(m.next?.sign).toBe(-2);
  });

  it("announces kaccha road within 300 m", () => {
    const start = r.cum[2]!;
    expect(surfaceAhead(r, start - 500)).toBeNull();
    const a = surfaceAhead(r, start - 200)!;
    expect(a.kind).toBe("unpaved");
    expect(a.inM).toBeCloseTo(200, 0);
    expect(a.lengthM).toBeCloseTo(r.cum[5]! - start, 0);
    // Once on it, the remaining length counts down.
    expect(surfaceAhead(r, start + 100)!.inM).toBe(0);
  });

  it("needs 3 consecutive fixes over 40 m to call off-route", () => {
    const t = new OffRouteTracker();
    expect(t.update(60)).toBe(false);
    expect(t.update(60)).toBe(false);
    expect(t.update(10)).toBe(false); // back on route resets
    expect(t.update(60)).toBe(false);
    expect(t.update(60)).toBe(false);
    expect(t.update(60)).toBe(true);
    // Poor accuracy is forgiven up to 30 m.
    const u = new OffRouteTracker();
    expect([u.update(55, 30), u.update(55, 30), u.update(55, 30)]).toEqual([false, false, false]);
  });

  it("detects arrival and the community leg for the post-ride prompt", () => {
    expect(arrived(r, line[line.length - 1]!, r.total)).toBe(true);
    expect(arrived(r, line[0]!, 0)).toBe(false);
    expect(communityShortcut(scOption)).toBe("abc");
  });

  it("trims recorded ride ends", () => {
    const fixes = line.map((p, i) => ({ ...p, t: new Date(i * 1000).toISOString() }));
    const kept = trimFixes(fixes, 300, 300);
    expect(kept.length).toBeLessThan(fixes.length);
    expect(kept[0]).not.toEqual(fixes[0]);
  });
});
