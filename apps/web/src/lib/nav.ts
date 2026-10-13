import type { LatLng, Step, Trail } from "./api";
import { cumulative, project } from "./geo";

export type Prepared = { trail: Trail; cum: number[]; total: number };

export function prepare(trail: Trail): Prepared {
  const cum = cumulative(trail.geometry);
  return { trail, cum, total: cum[cum.length - 1] || trail.distance_m };
}

export type NavState = {
  along: number; // metres from start
  offBy: number; // metres off the line
  offRoute: boolean;
  step: Step | null; // next manoeuvre
  toStepM: number;
  remainingM: number;
  remainingS: number;
  ahead: string | null; // "Kaccha starts in 300 m", "Sirpur Lake in 1.2 km"
  arrived: boolean;
};

/** Where are we on the trail, and what's next? */
export function locate(p: Prepared, pos: LatLng, prevAlong = 0, accuracyM = 15): NavState {
  const pr = project(p.trail.geometry, p.cum, pos);
  // Don't jump backwards on loops/out-and-back roads unless clearly so.
  const along = pr.alongM < prevAlong - 80 ? prevAlong : pr.alongM;
  return stateAt(p, along, pr.distM, accuracyM);
}

export function stateAt(p: Prepared, along: number, offBy = 0, accuracyM = 15): NavState {
  const t = p.trail;
  const steps = t.steps ?? [];
  const step = steps.find((s) => s.along_m > along + 8) ?? steps[steps.length - 1] ?? null;
  const toStepM = step ? Math.max(0, step.along_m - along) : 0;
  const remainingM = Math.max(0, p.total - along);
  const remainingS = t.duration_s * (remainingM / (p.total || 1));
  return {
    along,
    offBy,
    offRoute: offBy > Math.max(45, accuracyM * 1.5),
    step,
    toStepM,
    remainingM,
    remainingS,
    ahead: aheadNote(t, along),
    arrived: remainingM < 25,
  };
}

function fmt(m: number): string {
  return m >= 950 ? `${(m / 1000).toFixed(1)} km` : `${Math.max(10, Math.round(m / 10) * 10)} m`;
}

/** The most useful thing coming up within the next stretch. */
export function aheadNote(t: Trail, along: number): string | null {
  const cur = t.surfaces.find((s) => s.from_m <= along && s.to_m > along);
  const next = t.surfaces.find((s) => s.from_m > along && s.from_m - along < 1200 && s.kind !== cur?.kind);
  if (next && (next.kind === "dirt" || next.kind === "rough") && cur?.kind === "paved") {
    return `${next.kind === "dirt" ? "Kaccha" : "Gravel"} starts in ${fmt(next.from_m - along)}`;
  }
  if (next && next.kind === "paved" && cur && cur.kind !== "paved") return `Tarmac again in ${fmt(next.from_m - along)}`;
  const hl = (t.highlights ?? []).find((h) => h.label && h.from_m > along && h.from_m - along < 2500 && h.kind !== "twisty");
  if (hl) return `${hl.label} in ${fmt(hl.from_m - along)}`;
  if (cur && cur.kind !== "paved") return `On ${cur.kind === "dirt" ? "kaccha" : cur.kind} for ${fmt(cur.to_m - along)}`;
  return null;
}

/** Spoken prompt for a manoeuvre at a distance. */
export function prompt(step: Step, toM: number): string {
  const text = step.text.replace(/^Continue onto/, "Continue on");
  if (toM < 60) return text;
  return `In ${fmt(toM)}, ${text.charAt(0).toLowerCase()}${text.slice(1)}`;
}
