import { useQuery } from "@tanstack/react-query";
import { api, type Region } from "./api";
import { useApp } from "../state/store";

/** Region centre as a point (the API sends [lat, lng]). */
export function regionCenter(r: Region): { lat: number; lng: number } {
  return { lat: r.center[0] ?? 0, lng: r.center[1] ?? 0 };
}

export function useRegions() {
  return useQuery({ queryKey: ["regions"], queryFn: api.regions, staleTime: 60 * 60 * 1000 });
}

/** The selected region (falls back to the first seeded region). */
export function useRegion(): Region | undefined {
  const id = useApp((s) => s.regionId);
  const { data } = useRegions();
  const list = data?.regions ?? [];
  return list.find((r) => r.id === id) ?? list.find((r) => r.seeded) ?? list[0];
}

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api.me();
      } catch (e) {
        if ((e as { status?: number }).status === 401) return null;
        throw e;
      }
    },
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

export function useTilesManifest() {
  return useQuery({
    queryKey: ["tiles-manifest"],
    queryFn: async () => {
      const r = await fetch("/tiles/manifest.json");
      if (!r.ok) return null;
      return (await r.json()) as Record<string, unknown> & {
        packs?: Record<string, { file: string; bytes: number; radius_km: number }>;
      };
    },
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
}
