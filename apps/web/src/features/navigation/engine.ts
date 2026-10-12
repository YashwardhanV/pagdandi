import type { Instruction, RouteOption } from "../../lib/api";
import { cumulative, decodePolyline, haversine, project, type LatLng } from "../../lib/geo";

export type NavRoute = {
  option: RouteOption;
  line: LatLng[];
  cum: number[];
  total: number;
};

export function prepare(option: RouteOption): NavRoute {
  const line = decodePolyline(option.polyline);
  const cum = cumulative(line);
  return { option, line, cum, total: cum[cum.length - 1] ?? 0 };
}

export type Progress = { alongM: number; offM: number; segment: number; remainingM: number };

export function progress(r: NavRoute, p: LatLng): Progress {
  const pr = project(r.line, p);
  return { alongM: pr.alongM, offM: pr.distM, segment: pr.segment, remainingM: Math.max(0, r.total - pr.alongM) };
}

export type Maneuver = { instruction: Instruction; distanceM: number; next?: Instruction };

/** The next maneuver ahead: the first instruction whose start point is beyond our position. */
export function nextManeuver(r: NavRoute, alongM: number): Maneuver | null {
  const ins = r.option.instructions;
  for (let i = 0; i < ins.length; i++) {
    const at = r.cum[ins[i]!.interval[0] ?? 0] ?? 0;
    if (at > alongM + 3) return { instruction: ins[i]!, distanceM: at - alongM, next: ins[i + 1] };
  }
  const last = ins[ins.length - 1];
  return last ? { instruction: last, distanceM: Math.max(0, r.total - alongM) } : null;
}

export type SurfaceAhead = { kind: "unpaved" | "track"; inM: number; lengthM: number; startIdx: number };

/** The next unpaved/track stretch starting within `withinM` metres (SPEC §6.4, §10.2.4). */
export function surfaceAhead(r: NavRoute, alongM: number, withinM = 300): SurfaceAhead | null {
  for (const s of r.option.surface_spans) {
    if (s.kind === "paved") continue;
    const start = r.cum[s.from_idx] ?? 0;
    const end = r.cum[s.to_idx] ?? start;
    if (end <= alongM) continue;
    const inM = start - alongM;
    if (inM > withinM) return null;
    return { kind: s.kind, inM: Math.max(0, inM), lengthM: end - Math.max(start, alongM), startIdx: s.from_idx };
  }
  return null;
}

/**
 * Off-route detection: more than 40 m from the route for 3 consecutive fixes
 * (SPEC §6.4). Fixes with poor accuracy count only if even their best case is off.
 */
export class OffRouteTracker {
  private count = 0;
  constructor(
    private readonly thresholdM = 40,
    private readonly fixes = 3,
  ) {}
  update(offM: number, accuracyM = 0): boolean {
    const off = offM - Math.min(accuracyM, 30) > this.thresholdM;
    this.count = off ? this.count + 1 : 0;
    return this.count >= this.fixes;
  }
  reset() {
    this.count = 0;
  }
}

export function arrived(r: NavRoute, p: LatLng, alongM: number): boolean {
  const end = r.line[r.line.length - 1];
  return !!end && (haversine(p, end) < 30 || r.total - alongM < 20);
}

/** Community shortcut used by the option, if any. */
export function communityShortcut(option: RouteOption): string | null {
  return option.legs.find((l) => l.source === "community")?.shortcut_id ?? null;
}
