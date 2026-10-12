import { useEffect, useState } from "react";
import { useApp } from "../state/store";
import { useRegion } from "./queries";
import { isDaylight } from "./time";

/**
 * useScheme resolves the colour scheme: the user's choice, or in auto mode
 * dark between local sunset and sunrise (SPEC §10.2, §11.3). It also sets
 * <html data-theme> so tokens switch.
 */
export function useScheme(): "light" | "dark" {
  const theme = useApp((s) => s.theme);
  const pos = useApp((s) => s.userPos);
  const region = useRegion();
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setTick((x) => x + 1), 5 * 60 * 1000);
    return () => clearInterval(t);
  }, []);
  let scheme: "light" | "dark" = "light";
  if (theme === "light" || theme === "dark") scheme = theme;
  else {
    const lat = pos?.lat ?? region?.center[0];
    const lng = pos?.lng ?? region?.center[1];
    if (lat !== undefined && lng !== undefined) scheme = isDaylight(new Date(), lat, lng) ? "light" : "dark";
  }
  void tick;
  useEffect(() => {
    document.documentElement.dataset.theme = scheme;
    const bg = getComputedStyle(document.documentElement).getPropertyValue("--bg").trim();
    document.querySelectorAll('meta[name="theme-color"]').forEach((m) => m.setAttribute("content", bg));
  }, [scheme]);
  return scheme;
}
