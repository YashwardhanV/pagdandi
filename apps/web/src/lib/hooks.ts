import { useEffect, useState } from "react";

export function useMediaQuery(q: string): boolean {
  const get = () => (typeof window !== "undefined" && "matchMedia" in window ? window.matchMedia(q).matches : false);
  const [m, setM] = useState(get);
  useEffect(() => {
    if (!("matchMedia" in window)) return;
    const mq = window.matchMedia(q);
    const on = () => setM(mq.matches);
    on();
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, [q]);
  return m;
}

export const useDesktop = () => useMediaQuery("(min-width: 1024px)");

export function useOnline(): boolean {
  const [on, setOn] = useState(typeof navigator === "undefined" ? true : navigator.onLine);
  useEffect(() => {
    const up = () => setOn(true);
    const down = () => setOn(false);
    window.addEventListener("online", up);
    window.addEventListener("offline", down);
    return () => {
      window.removeEventListener("online", up);
      window.removeEventListener("offline", down);
    };
  }, []);
  return on;
}

export function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

export const prefersReducedMotion = () =>
  typeof window !== "undefined" && "matchMedia" in window && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
