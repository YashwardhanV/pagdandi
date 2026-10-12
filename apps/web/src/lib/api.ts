import createClient from "openapi-fetch";
import type { components, paths } from "./api-types";

export type Schemas = components["schemas"];
export type Region = Schemas["Region"];
export type Destination = Schemas["Destination"];
export type Place = Schemas["Place"];
export type RouteRequest = Schemas["RouteRequest"];
export type RouteResponse = Schemas["RouteResponse"];
export type RouteOption = Schemas["RouteOption"];
export type Warning = Schemas["Warning"];
export type Instruction = Schemas["Instruction"];
export type Shortcut = Schemas["Shortcut"];
export type ShortcutDetail = Schemas["ShortcutDetail"];
export type NewShortcut = Schemas["NewShortcut"];
export type Candidate = Schemas["Candidate"];
export type User = Schemas["User"];
export type Vehicle = Schemas["Vehicle"];
export type Mode = Schemas["Mode"];
export type Surface = Schemas["Surface"];
export type Flag = Schemas["Flag"];
export type SnapResult = Schemas["SnapResult"];

const base = import.meta.env.VITE_API_BASE ?? "";

export const client = createClient<paths>({ baseUrl: base, credentials: "include" });

/** ApiError carries the server's error envelope. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: Record<string, unknown> | undefined;
  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.status = status;
    this.code = code;
    this.details = details;
  }
  get offline(): boolean {
    return this.code === "OFFLINE";
  }
}

type Result<T> = { data?: T; error?: unknown; response: Response };

/** unwrap turns an openapi-fetch result into data or a thrown ApiError. */
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  let r: Result<T>;
  try {
    r = await p;
  } catch {
    throw new ApiError(0, "OFFLINE", navigator.onLine ? "Could not reach Pagdandi. Check your connection." : "You are offline.");
  }
  if (r.response.ok) return r.data as T;
  const env = r.error as { error?: { code?: string; message?: string; details?: Record<string, unknown> } } | undefined;
  throw new ApiError(
    r.response.status,
    env?.error?.code ?? `HTTP_${r.response.status}`,
    env?.error?.message ?? "Something went wrong. Please try again.",
    env?.error?.details,
  );
}

/** api groups the calls the app makes. */
export const api = {
  regions: () => unwrap(client.GET("/v1/regions")),
  destinations: (id: string) => unwrap(client.GET("/v1/regions/{id}/destinations", { params: { path: { id } } })),
  geocode: (q: string, near?: { lat: number; lng: number }, lang?: "en" | "hi" | "sv") =>
    unwrap(client.GET("/v1/geocode", { params: { query: { q, lat: near?.lat, lng: near?.lng, lang } } })),
  reverse: (lat: number, lng: number) => unwrap(client.GET("/v1/reverse", { params: { query: { lat, lng } } })),
  route: (body: RouteRequest, signal?: AbortSignal) => unwrap(client.POST("/v1/route", { body, signal })),
  gpxUrl: (id: string, option: string) => `${base}/v1/route/${encodeURIComponent(id)}/gpx?option=${encodeURIComponent(option)}`,
  shortcuts: (bbox: [number, number, number, number], zoom: number, adventure = false) =>
    unwrap(
      client.GET("/v1/shortcuts", {
        params: { query: { bbox: bbox.map((v) => v.toFixed(5)).join(","), zoom, adventure: adventure ? "1" : "0" } },
      }),
    ),
  shortcut: (id: string) => unwrap(client.GET("/v1/shortcuts/{id}", { params: { path: { id } } })),
  createShortcut: (body: NewShortcut) => unwrap(client.POST("/v1/shortcuts", { body })),
  snap: (lat: number, lng: number) => unwrap(client.POST("/v1/shortcuts/snap", { body: { lat, lng } })),
  report: (id: string, body: Schemas["NewReport"]) => unwrap(client.POST("/v1/shortcuts/{id}/reports", { params: { path: { id } }, body })),
  patchShortcut: (id: string, body: { title?: string; flags?: Flag[]; vehicles?: Vehicle[]; surface?: Surface }) =>
    unwrap(client.PATCH("/v1/shortcuts/{id}", { params: { path: { id } }, body })),
  candidates: (bbox: [number, number, number, number]) =>
    unwrap(client.GET("/v1/candidates", { params: { query: { bbox: bbox.map((v) => v.toFixed(5)).join(",") } } })),
  answerCandidate: (id: string, yes: boolean) =>
    unwrap(client.POST("/v1/candidates/{id}/answer", { params: { path: { id } }, body: { yes } })),
  uploadTrace: (points: Schemas["TimedPoint"][]) => unwrap(client.POST("/v1/traces", { body: { points } })),
  me: () => unwrap(client.GET("/v1/me")),
  patchMe: (body: { display_name?: string; home_region?: string; traces_consent?: boolean }) =>
    unwrap(client.PATCH("/v1/me", { body })),
  deleteMe: () => unwrap(client.DELETE("/v1/me")),
  contributions: () => unwrap(client.GET("/v1/me/contributions")),
  places: () => unwrap(client.GET("/v1/me/places")),
  putPlace: (kind: "home" | "work", lat: number, lng: number) =>
    unwrap(client.PUT("/v1/me/places/{kind}", { params: { path: { kind } }, body: { lat, lng } })),
  providers: () => unwrap(client.GET("/v1/auth/providers")),
  magicLink: (email: string, redirect?: string) => unwrap(client.POST("/v1/auth/magic-link", { body: { email, redirect } })),
  logout: () => unwrap(client.POST("/v1/auth/logout")),
  googleStartUrl: (redirect?: string) => `${base}/v1/auth/google/start${redirect ? `?redirect=${encodeURIComponent(redirect)}` : ""}`,
  adminQueue: () => unwrap(client.GET("/v1/admin/queue")),
  adminAction: (id: string, body: { action: "approve" | "remove" | "edit_flags" | "dismiss"; flags?: Flag[]; note?: string }) =>
    unwrap(client.POST("/v1/admin/shortcuts/{id}/action", { params: { path: { id } }, body })),
};
