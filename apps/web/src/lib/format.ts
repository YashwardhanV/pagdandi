export function km(m: number, digits?: number): string {
  if (m < 950 && digits === undefined) return `${Math.round(m / 10) * 10} m`;
  const v = m / 1000;
  const d = digits ?? (v < 10 ? 1 : 0);
  return `${v.toFixed(d)} km`;
}

export function kmNum(m: number): string {
  const v = m / 1000;
  return v < 10 ? v.toFixed(1) : v.toFixed(0);
}

export function duration(s: number): string {
  const m = Math.max(1, Math.round(s / 60));
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  const r = m % 60;
  return r ? `${h} h ${r} min` : `${h} h`;
}

/** "+4 min" / "−2 min" / "same time". */
export function deltaTime(s: number): string {
  const m = Math.round(s / 60);
  if (m === 0) return "same time";
  return `${m > 0 ? "+" : "−"}${Math.abs(m)} min`;
}

export function deltaDist(m: number): string {
  if (Math.abs(m) < 100) return "same distance";
  return `${m > 0 ? "+" : "−"}${km(Math.abs(m))}`;
}

export function clock(d: Date): string {
  return d.toLocaleTimeString("en-IN", { hour: "numeric", minute: "2-digit" });
}

export function pct(v: number): string {
  return `${Math.round(v * 100)}%`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
export function monthLabel(ym?: string): string {
  if (!ym || ym.length !== 7) return ym ?? "";
  const m = Number(ym.slice(5));
  return `${MONTHS[m - 1] ?? ""} ${ym.slice(0, 4)}`;
}
