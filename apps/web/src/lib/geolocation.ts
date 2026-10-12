import { useApp, type UserPos } from "../state/store";

function toPos(p: GeolocationPosition): UserPos {
  return {
    lat: p.coords.latitude,
    lng: p.coords.longitude,
    accuracy: p.coords.accuracy,
    heading: p.coords.heading ?? null,
    speed: p.coords.speed ?? null,
    at: p.timestamp,
  };
}

/** Whether the browser will show a permission prompt (we explain first, SPEC §10.3). */
export async function needsExplanation(): Promise<boolean> {
  if (!("permissions" in navigator)) return true;
  try {
    const s = await navigator.permissions.query({ name: "geolocation" as PermissionName });
    return s.state === "prompt";
  } catch {
    return true;
  }
}

/** One fix; stores it in app state. Only call after the user tapped Locate or Ride. */
export function locateOnce(): Promise<UserPos> {
  return new Promise((resolve, reject) => {
    if (!("geolocation" in navigator)) return reject(new Error("unsupported"));
    navigator.geolocation.getCurrentPosition(
      (p) => {
        const pos = toPos(p);
        useApp.getState().set({ userPos: pos, geoError: null });
        resolve(pos);
      },
      (e) => {
        useApp.getState().set({ geoError: e.code === e.PERMISSION_DENIED ? "denied" : "unavailable" });
        reject(e);
      },
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 10000 },
    );
  });
}

/** Continuous fixes for navigation and ride recording. Returns a stop function. */
export function watchPosition(onFix: (p: UserPos) => void, onError?: (e: GeolocationPositionError) => void): () => void {
  if (!("geolocation" in navigator)) return () => {};
  const id = navigator.geolocation.watchPosition(
    (p) => {
      const pos = toPos(p);
      useApp.getState().set({ userPos: pos, geoError: null });
      onFix(pos);
    },
    (e) => {
      useApp.getState().set({ geoError: e.code === e.PERMISSION_DENIED ? "denied" : "unavailable" });
      onError?.(e);
    },
    { enableHighAccuracy: true, maximumAge: 2000, timeout: 20000 },
  );
  return () => navigator.geolocation.clearWatch(id);
}
