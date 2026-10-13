// Typed client for the Pagdandi API (services/api/internal/server).

export type LatLng = { lat: number; lng: number };
export type VehicleId = "bike" | "scooty" | "car" | "cycle" | "walk" | "jetski";
export type Flavour = "any" | "water" | "hills" | "forest" | "heritage" | "wild" | "fresh";
export type TrailKind = "usual" | "shortest" | "kaccha" | "quiet" | "scenic" | "fresh" | "loop" | "open" | "shore";

export type Vehicle = {
  id: VehicleId;
  label: string;
  label_hi: string;
  water: boolean;
  motor: boolean;
  cruise_kmh: number;
  max_km: number;
};

export type Meta = {
  region: {
    id: string;
    name: string;
    name_hi: string;
    center: LatLng;
    radius_km: number;
    bbox: [number, number, number, number];
    ring: [number, number][];
    timezone: string;
  };
  vehicles: Vehicle[];
  flavours: Flavour[];
  data: { osm_date: string; built_at: string; fresh_months: number; counts: Record<string, number> };
  tiles: { url: string; available: boolean };
};

export type Span = { kind: string; from_m: number; to_m: number; label?: string };
export type Note = { kind: string; text: string };
export type Step = {
  text: string;
  street?: string;
  sign: number;
  distance_m: number;
  duration_s: number;
  at: [number, number];
  index: number;
  along_m: number;
};
export type Via = { name: string; kind: string; lat: number; lng: number };

export type Trail = {
  id: string;
  kind: TrailKind;
  /** Client-side colour override (loops get one each). */
  tint?: TrailKind;
  also?: TrailKind[];
  name: string;
  name_hi: string;
  tagline: string;
  distance_m: number;
  duration_s: number;
  delta_m: number;
  delta_s: number;
  climb_m: number;
  descent_m: number;
  geometry: [number, number][];
  elevation: [number, number][] | null;
  surfaces: Span[];
  mix: Record<string, number>;
  quiet: number;
  scenic: number;
  twisty: number;
  backroads_m: number;
  fresh_m: number;
  fresh_since?: string;
  fresh_spans?: Span[];
  highlights?: Span[];
  warnings?: Note[];
  steps?: Step[];
  via?: Via[];
  bbox: [number, number, number, number];
};

export type GoResult = { vehicle: VehicleId; stray_km: number; trails: Trail[]; notes?: Note[]; took_ms: number };

export type Place = {
  id: string;
  name: string;
  name_hi?: string;
  kind: string;
  lat: number;
  lng: number;
  target: LatLng;
  distance_km: number;
  bearing: number;
  score: number;
  why: string;
  tags?: string[];
};

export type WanderResult = {
  vehicle: VehicleId;
  radius_km: number;
  flavour: Flavour;
  kind: "places" | "loops";
  places?: Place[];
  loops?: Trail[];
  notes?: Note[];
  took_ms: number;
};

export type SearchPlace = {
  id: string;
  name: string;
  label: string;
  kind: string;
  lat: number;
  lng: number;
  source: "gem" | "osm" | "pin";
  inside: boolean;
};

export type Gem = {
  id: string;
  name: string;
  name_hi?: string;
  kind: string;
  lat: number;
  lng: number;
  access?: LatLng;
  score: number;
  featured?: boolean;
  wiki?: boolean;
  area_km2?: number;
  ele_m?: number;
};

export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

const BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, { ...init, headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) } });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError("offline", "Can't reach Pagdandi. Check your connection.", 0);
  }
  if (!res.ok) {
    let code = "error";
    let message = `Something went wrong (${res.status})`;
    try {
      const body = await res.json();
      code = body?.error?.code ?? code;
      message = body?.error?.message ?? message;
    } catch {
      /* not JSON */
    }
    throw new ApiError(code, message, res.status);
  }
  return res.json() as Promise<T>;
}

export const api = {
  meta: (signal?: AbortSignal) => req<Meta>("/v1/meta", { signal }),
  search: (q: string, near: LatLng | null, signal?: AbortSignal) => {
    const p = new URLSearchParams({ q });
    if (near) {
      p.set("lat", near.lat.toFixed(5));
      p.set("lng", near.lng.toFixed(5));
    }
    return req<{ places: SearchPlace[] }>(`/v1/search?${p}`, { signal });
  },
  reverse: (at: LatLng, signal?: AbortSignal) =>
    req<SearchPlace>(`/v1/reverse?lat=${at.lat.toFixed(6)}&lng=${at.lng.toFixed(6)}`, { signal }),
  go: (
    body: { from: LatLng; to: LatLng; vehicle: VehicleId; stray_km?: number | null },
    signal?: AbortSignal,
  ) => {
    const b: Record<string, unknown> = { from: body.from, to: body.to, vehicle: body.vehicle };
    if (body.stray_km != null) b.stray_km = body.stray_km;
    return req<GoResult>("/v1/go", { method: "POST", body: JSON.stringify(b), signal });
  },
  wander: (
    body: { from: LatLng; vehicle: VehicleId; radius_km: number; flavour: Flavour; kind: "places" | "loops"; seed: number },
    signal?: AbortSignal,
  ) => req<WanderResult>("/v1/wander", { method: "POST", body: JSON.stringify(body), signal }),
  gems: (signal?: AbortSignal) => req<{ gems: Gem[] }>("/v1/gems?limit=300", { signal }),
  fresh: (bbox: [number, number, number, number], signal?: AbortSignal) =>
    req<GeoJSON.FeatureCollection>(`/v1/fresh?bbox=${bbox.map((v) => v.toFixed(4)).join(",")}`, { signal }),
};
