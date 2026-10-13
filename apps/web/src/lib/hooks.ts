import { useEffect, useState } from "react";
import { api, type Gem } from "./api";

let gemsCache: Promise<Gem[]> | null = null;

/** Gems (destinations) for the whole region, fetched once. */
export function useGems(): Gem[] {
  const [gems, setGems] = useState<Gem[]>([]);
  useEffect(() => {
    gemsCache ??= api.gems().then((r) => r.gems).catch(() => {
      gemsCache = null;
      return [];
    });
    let live = true;
    void gemsCache.then((g) => live && setGems(g));
    return () => {
      live = false;
    };
  }, []);
  return gems;
}

export function useMedia(q: string): boolean {
  const [m, setM] = useState(() => window.matchMedia(q).matches);
  useEffect(() => {
    const mq = window.matchMedia(q);
    const on = () => setM(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, [q]);
  return m;
}
