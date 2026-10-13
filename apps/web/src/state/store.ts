import { create } from "zustand";
import { api, ApiError, type Flavour, type GoResult, type LatLng, type Meta, type Place, type Trail, type VehicleId, type WanderResult } from "../lib/api";
import { load, save } from "../lib/storage";
import { isDark } from "../lib/sun";
import { LOOP_TINTS } from "../lib/kinds";

export type Mode = "go" | "wander" | "ride";
export type Theme = "auto" | "dusk" | "day";
export type Base = "atlas" | "satellite";
export type Endpoint = { name: string; sub?: string; at: LatLng; kind?: string; isMe?: boolean };
export type Status = "idle" | "loading" | "done" | "error";
export type Camera = { bbox?: [number, number, number, number]; center?: LatLng; zoom?: number; padding?: "sheet" | "none"; nonce: number };

type GoState = { status: Status; result: GoResult | null; error: string | null; code?: string };
type WanderState = {
  radiusKm: number;
  flavour: Flavour;
  tab: "places" | "loops";
  seed: number;
  status: Status;
  result: WanderResult | null;
  error: string | null;
};

export type State = {
  meta: Meta | null;
  metaError: string | null;
  mode: Mode;
  vehicle: VehicleId;
  theme: Theme;
  resolvedTheme: "dusk" | "day";
  base: Base;
  terrain3d: boolean;
  showFresh: boolean;
  me: LatLng | null;
  meAccuracy: number | null;
  locating: boolean;
  from: Endpoint | null;
  to: Endpoint | null;
  stray: number | null;
  go: GoState;
  selected: string | null;
  detail: boolean;
  wander: WanderState;
  ride: { trail: Trail | null; simulate: boolean };
  search: "to" | "from" | null;
  layers: boolean;
  about: boolean;
  toast: { text: string; tone: "info" | "warn" } | null;
  camera: Camera | null;
  recent: Endpoint[];
};

type Actions = {
  init: () => Promise<void>;
  setMode: (m: Mode) => void;
  setVehicle: (v: VehicleId) => void;
  setTheme: (t: Theme) => void;
  tickTheme: () => void;
  setBase: (b: Base) => void;
  toggle3d: () => void;
  toggleFresh: () => void;
  locate: (silent?: boolean) => void;
  setTo: (e: Endpoint | null) => void;
  setFrom: (e: Endpoint | null) => void;
  swap: () => void;
  setStray: (km: number | null) => void;
  plan: () => Promise<void>;
  select: (id: string | null) => void;
  setDetail: (open: boolean) => void;
  setWander: (p: Partial<Pick<WanderState, "radiusKm" | "flavour" | "tab">>) => void;
  shuffle: () => void;
  explore: () => Promise<void>;
  goToPlace: (p: Place) => void;
  startRide: (t: Trail, simulate: boolean) => void;
  endRide: () => void;
  openSearch: (which: "to" | "from" | null) => void;
  setLayers: (open: boolean) => void;
  setAbout: (open: boolean) => void;
  showToast: (text: string, tone?: "info" | "warn") => void;
  fly: (c: Omit<Camera, "nonce">) => void;
  clearTrip: () => void;
};

let goAbort: AbortController | null = null;
let wanderAbort: AbortController | null = null;
let toastTimer: ReturnType<typeof setTimeout> | undefined;

/** Where trips start: the chosen origin, else GPS, else the city centre. */
export function origin(s: State): Endpoint | null {
  if (s.from) return s.from;
  if (s.me) return { name: "Your location", at: s.me, isMe: true };
  if (s.meta) return { name: `${s.meta.region.name} centre`, sub: "Turn on location for your real start", at: s.meta.region.center };
  return null;
}

export const selectedTrail = (s: State): Trail | null => {
  const list = s.mode === "wander" ? s.wander.result?.loops ?? [] : s.go.result?.trails ?? [];
  return list.find((t) => t.id === s.selected) ?? list[0] ?? null;
};

export const useStore = create<State & Actions>((set, get) => ({
  meta: null,
  metaError: null,
  mode: "go",
  vehicle: load<VehicleId>("vehicle", "bike"),
  theme: load<Theme>("theme", "auto"),
  resolvedTheme: "dusk",
  base: load<Base>("base", "atlas"),
  terrain3d: false,
  showFresh: load<boolean>("showFresh", false),
  me: null,
  meAccuracy: null,
  locating: false,
  from: null,
  to: null,
  stray: null,
  go: { status: "idle", result: null, error: null },
  selected: null,
  detail: false,
  wander: { radiusKm: load<number>("radius", 25), flavour: "any", tab: "places", seed: 0, status: "idle", result: null, error: null },
  ride: { trail: null, simulate: false },
  search: null,
  layers: false,
  about: false,
  toast: null,
  camera: null,
  recent: load<Endpoint[]>("recent", []),

  init: async () => {
    get().tickTheme();
    try {
      const meta = await api.meta();
      set({ meta, metaError: null });
      get().tickTheme();
    } catch (e) {
      set({ metaError: (e as Error).message });
    }
    get().locate(true);
  },

  setMode: (mode) => {
    const s = get();
    if (mode === s.mode) return;
    set({ mode, selected: null, detail: false });
    if (mode === "wander") {
      const o = origin(get());
      if (o) get().fly({ center: o.at, zoom: zoomForRadius(get().wander.radiusKm) });
      if (!get().wander.result) void get().explore();
    } else if (mode === "go") {
      const r = get().go.result;
      if (r?.trails[0]) get().fly({ bbox: unionBBox(r.trails), padding: "sheet" });
    }
  },

  setVehicle: (vehicle) => {
    if (vehicle === get().vehicle) return;
    save("vehicle", vehicle);
    set({ vehicle });
    if (get().mode === "go" && get().to) void get().plan();
    if (get().mode === "wander") void get().explore();
  },

  setTheme: (theme) => {
    save("theme", theme);
    set({ theme });
    get().tickTheme();
  },

  tickTheme: () => {
    const { theme, meta } = get();
    let resolved: "dusk" | "day" = theme === "day" ? "day" : "dusk";
    if (theme === "auto") {
      const c = meta?.region.center ?? { lat: 22.72, lng: 75.86 };
      resolved = isDark(new Date(), c.lat, c.lng) ? "dusk" : "day";
    }
    if (resolved !== get().resolvedTheme) set({ resolvedTheme: resolved });
    document.documentElement.dataset.theme = resolved;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", resolved === "dusk" ? "#0B0F15" : "#F4EFE4");
  },

  setBase: (base) => {
    save("base", base);
    set({ base });
  },
  toggle3d: () => set({ terrain3d: !get().terrain3d }),
  toggleFresh: () => {
    save("showFresh", !get().showFresh);
    set({ showFresh: !get().showFresh });
  },

  locate: (silent) => {
    if (!("geolocation" in navigator)) {
      if (!silent) get().showToast("This device can't share its location", "warn");
      return;
    }
    set({ locating: true });
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        const me = { lat: pos.coords.latitude, lng: pos.coords.longitude };
        const meta = get().meta;
        const inside = !meta || haversineKm(me, meta.region.center) <= meta.region.radius_km;
        const first = get().me == null;
        set({ me: inside ? me : null, meAccuracy: pos.coords.accuracy, locating: false });
        if (!inside) {
          if (!silent) get().showToast(`You're outside the ${meta?.region.name} area. Starting from the city centre.`, "warn");
          return;
        }
        const s = get();
        // The start just moved from the city centre to you: refresh what depends on it.
        if (first && !s.from) {
          if (s.mode === "wander") void s.explore();
          else if (s.mode === "go" && s.to) void s.plan();
        }
        if (s.mode === "wander") {
          if (!s.from) s.fly({ center: me, zoom: zoomForRadius(s.wander.radiusKm) });
        } else if (s.mode === "go" && (!silent || !s.to)) s.fly({ center: me, zoom: 14 });
      },
      () => {
        set({ locating: false });
        if (!silent) get().showToast("Location is off. Starting from the city centre.", "warn");
      },
      { enableHighAccuracy: true, timeout: 12000, maximumAge: 30000 },
    );
  },

  setTo: (to) => {
    set({ to, search: null, selected: null, detail: false });
    if (to) {
      remember(to, set, get);
      if (get().mode !== "go") set({ mode: "go" });
      void get().plan();
    } else {
      goAbort?.abort();
      set({ go: { status: "idle", result: null, error: null } });
    }
  },

  setFrom: (from) => {
    set({ from, search: null, selected: null });
    if (from) remember(from, set, get);
    if (get().to && get().mode === "go") void get().plan();
    if (get().mode === "wander") void get().explore();
  },

  swap: () => {
    const s = get();
    const o = origin(s);
    if (!s.to || !o) return;
    set({ from: s.to, to: o.isMe ? { ...o, name: "Your location" } : o });
    void get().plan();
  },

  setStray: (stray) => {
    set({ stray });
    if (get().to) void get().plan();
  },

  plan: async () => {
    const s = get();
    const o = origin(s);
    if (!o || !s.to) return;
    goAbort?.abort();
    const ac = new AbortController();
    goAbort = ac;
    set({ go: { ...s.go, status: "loading", error: null }, selected: null, detail: false });
    try {
      const result = await api.go({ from: o.at, to: s.to.at, vehicle: s.vehicle, stray_km: s.stray }, ac.signal);
      if (ac.signal.aborted) return;
      set({ go: { status: "done", result, error: null }, selected: result.trails[0]?.id ?? null });
      if (result.trails.length) get().fly({ bbox: unionBBox(result.trails), padding: "sheet" });
    } catch (e) {
      if ((e as Error).name === "AbortError") return;
      const err = e as ApiError;
      set({ go: { status: "error", result: null, error: err.message, code: err.code } });
    }
  },

  select: (selected) => {
    set({ selected });
    const t = selectedTrail(get());
    if (t) get().fly({ bbox: t.bbox, padding: "sheet" });
  },

  setDetail: (detail) => set({ detail }),

  setWander: (p) => {
    const w = { ...get().wander, ...p };
    if (p.radiusKm !== undefined) save("radius", p.radiusKm);
    set({ wander: w, selected: null });
    if (p.radiusKm !== undefined) {
      const o = origin(get());
      if (o) get().fly({ center: o.at, zoom: zoomForRadius(w.radiusKm) });
    }
    void get().explore();
  },

  shuffle: () => {
    set({ wander: { ...get().wander, seed: Math.floor(Math.random() * 1e9) + 1 } });
    void get().explore();
  },

  explore: async () => {
    const s = get();
    const o = origin(s);
    if (!o) return;
    wanderAbort?.abort();
    const ac = new AbortController();
    wanderAbort = ac;
    const w = s.wander;
    set({ wander: { ...w, status: "loading", error: null } });
    try {
      const result = await api.wander(
        { from: o.at, vehicle: s.vehicle, radius_km: w.radiusKm, flavour: w.flavour, kind: w.tab, seed: w.seed },
        ac.signal,
      );
      if (ac.signal.aborted) return;
      if (result.loops) result.loops = result.loops.map((l, i) => ({ ...l, tint: LOOP_TINTS[i % LOOP_TINTS.length] }));
      set({ wander: { ...get().wander, status: "done", result, error: null }, selected: result.loops?.[0]?.id ?? null });
      if (w.tab === "loops" && result.loops?.length) get().fly({ bbox: unionBBox(result.loops), padding: "sheet" });
    } catch (e) {
      if ((e as Error).name === "AbortError") return;
      set({ wander: { ...get().wander, status: "error", result: null, error: (e as Error).message } });
    }
  },

  goToPlace: (p) => {
    if (get().vehicle === "jetski") {
      // A jetski can't ride to a lake by road: start on it and loop it.
      set({ from: { name: p.name, sub: p.why, at: p.target, kind: p.kind }, wander: { ...get().wander, tab: "loops", radiusKm: Math.min(get().wander.radiusKm, 10) } });
      get().fly({ center: p.target, zoom: 13 });
      void get().explore();
      return;
    }
    get().setTo({ name: p.name, sub: p.why, at: p.target, kind: p.kind });
  },

  startRide: (trail, simulate) => {
    set({ mode: "ride", ride: { trail, simulate }, detail: false });
  },

  endRide: () => {
    const back: Mode = get().wander.result?.loops?.some((l) => l.id === get().ride.trail?.id) ? "wander" : "go";
    set({ mode: back, ride: { trail: null, simulate: false } });
    const t = selectedTrail(get());
    if (t) get().fly({ bbox: t.bbox, padding: "sheet" });
  },

  openSearch: (search) => set({ search }),
  setLayers: (layers) => set({ layers }),
  setAbout: (about) => set({ about }),

  showToast: (text, tone = "info") => {
    clearTimeout(toastTimer);
    set({ toast: { text, tone } });
    toastTimer = setTimeout(() => set({ toast: null }), 4200);
  },

  fly: (c) => set({ camera: { ...c, nonce: Date.now() + Math.random() } }),

  clearTrip: () => {
    goAbort?.abort();
    set({ to: null, from: null, go: { status: "idle", result: null, error: null }, selected: null, detail: false });
    const o = origin(get());
    if (o) get().fly({ center: o.at, zoom: 13 });
  },
}));

function remember(e: Endpoint, set: (p: Partial<State>) => void, get: () => State) {
  if (e.isMe) return;
  const recent = [e, ...get().recent.filter((r) => r.name !== e.name)].slice(0, 8);
  save("recent", recent);
  set({ recent });
}

export function unionBBox(ts: { bbox: [number, number, number, number] }[]): [number, number, number, number] {
  return ts.reduce<[number, number, number, number]>(
    (b, t) => [Math.min(b[0], t.bbox[0]), Math.min(b[1], t.bbox[1]), Math.max(b[2], t.bbox[2]), Math.max(b[3], t.bbox[3])],
    [Infinity, Infinity, -Infinity, -Infinity],
  );
}

/** Zoom at which a circle of `km` fits the map area left visible by the panel. */
export function zoomForRadius(km: number, lat = 22.7): number {
  const w = window.innerWidth;
  const h = window.innerHeight;
  const desktop = w >= 900;
  const visible = desktop ? Math.min(w - 500, h - 120) : Math.min(w - 40, h * 0.44 - 110);
  const metres = 2 * km * 1000 * 1.12;
  const z = Math.log2((156543.03 * Math.cos((lat * Math.PI) / 180) * Math.max(160, visible)) / metres);
  return Math.max(6, Math.min(15, z));
}

function haversineKm(a: LatLng, b: LatLng): number {
  const r = (d: number) => (d * Math.PI) / 180;
  const s = Math.sin(r(b.lat - a.lat) / 2) ** 2 + Math.cos(r(a.lat)) * Math.cos(r(b.lat)) * Math.sin(r(b.lng - a.lng) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(s));
}
