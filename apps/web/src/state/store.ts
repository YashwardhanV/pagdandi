import { create } from "zustand";
import type { Mode, RouteResponse, Vehicle } from "../lib/api";
import { storage } from "../lib/storage";

export type Lang = "en" | "hi" | "sv";
export type Theme = "auto" | "light" | "dark";

export type Endpoint = { lat: number; lng: number; name: string; label?: string };
export type UserPos = { lat: number; lng: number; accuracy: number; heading: number | null; speed: number | null; at: number };

type Persisted = {
  lang: Lang;
  theme: Theme;
  vehicle: Vehicle | null;
  regionId: string;
  recents: Endpoint[];
  home: Endpoint | null;
  work: Endpoint | null;
  muted: boolean;
};

type State = Persisted & {
  from: Endpoint | null; // null = my location
  to: Endpoint | null;
  mode: Mode;
  route: RouteResponse | null;
  selected: string | null;
  userPos: UserPos | null;
  geoError: string | null;
  set: (p: Partial<State>) => void;
  addRecent: (e: Endpoint) => void;
};

function defaultLang(): Lang {
  const n = typeof navigator !== "undefined" ? navigator.language : "en";
  if (n.startsWith("hi")) return "hi";
  if (n.startsWith("sv")) return "sv";
  return "en";
}

const KEY = "pd.settings";
const saved = storage.get<Partial<Persisted>>(KEY, {});

export const useApp = create<State>((set, get) => ({
  lang: saved.lang ?? defaultLang(),
  theme: saved.theme ?? "auto",
  vehicle: saved.vehicle ?? null,
  regionId: saved.regionId ?? "indore",
  recents: saved.recents ?? [],
  home: saved.home ?? null,
  work: saved.work ?? null,
  muted: saved.muted ?? false,
  from: null,
  to: null,
  mode: "auto",
  route: null,
  selected: null,
  userPos: null,
  geoError: null,
  set: (p) => set(p),
  addRecent: (e) => {
    const rest = get().recents.filter((r) => Math.abs(r.lat - e.lat) > 1e-4 || Math.abs(r.lng - e.lng) > 1e-4);
    set({ recents: [e, ...rest].slice(0, 8) });
  },
}));

useApp.subscribe((s) => {
  const p: Persisted = {
    lang: s.lang, theme: s.theme, vehicle: s.vehicle, regionId: s.regionId,
    recents: s.recents, home: s.home, work: s.work, muted: s.muted,
  };
  storage.set(KEY, p);
});

/** Recent routes kept for offline viewing (SPEC §10.3: last 20). */
const ROUTES_KEY = "pd.routes";
export function rememberRoute(r: RouteResponse, from: Endpoint | null, to: Endpoint | null) {
  const list = storage.get<{ r: RouteResponse; from: Endpoint | null; to: Endpoint | null; at: number }[]>(ROUTES_KEY, []);
  storage.set(ROUTES_KEY, [{ r, from, to, at: Date.now() }, ...list.filter((x) => x.r.request_id !== r.request_id)].slice(0, 20));
}
export function recentRoutes() {
  return storage.get<{ r: RouteResponse; from: Endpoint | null; to: Endpoint | null; at: number }[]>(ROUTES_KEY, []);
}
