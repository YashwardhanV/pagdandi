import type { Lang } from "../state/store";
import { BCP47, translate } from "./i18n";

/** Distance with units always shown: "850 m", "7.1 km", "24 km". */
export function formatDistance(m: number, lang: Lang = "en"): string {
  const nf = (d: number) => new Intl.NumberFormat(BCP47[lang], { maximumFractionDigits: d, minimumFractionDigits: d });
  if (m < 1000) {
    const r = m < 100 ? Math.round(m / 10) * 10 : Math.round(m / 50) * 50;
    return `${nf(0).format(r)} ${translate(lang, "common.m")}`;
  }
  const km = m / 1000;
  return `${nf(km < 10 ? 1 : 0).format(km)} ${translate(lang, "common.km")}`;
}

/** Duration in minutes, or hours and minutes: "18 min", "1 h 05 min". */
export function formatDuration(s: number, lang: Lang = "en"): string {
  const min = Math.max(1, Math.round(s / 60));
  if (min < 60) return `${min} ${translate(lang, "common.min")}`;
  const h = Math.floor(min / 60);
  const rest = String(min % 60).padStart(2, "0");
  return `${h} ${translate(lang, "common.h")} ${rest} ${translate(lang, "common.min")}`;
}

/** Whole minutes, at least 1 (for "saves 9 min"). */
export const minutes = (s: number) => Math.max(1, Math.round(s / 60));

/** "3 days ago", "today", localised. */
export function formatAgo(iso: string | null | undefined, lang: Lang = "en", now = Date.now()): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  const rtf = new Intl.RelativeTimeFormat(BCP47[lang], { numeric: "auto" });
  const diff = (t - now) / 1000;
  const abs = Math.abs(diff);
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), "day");
  if (abs < 86400 * 365) return rtf.format(Math.round(diff / (86400 * 30)), "month");
  return rtf.format(Math.round(diff / (86400 * 365)), "year");
}

/** Short "3d ago" style for chips. */
export function formatAgoShort(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "";
  const s = (now - Date.parse(iso)) / 1000;
  if (!Number.isFinite(s)) return "";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

/** Clock time: "14:05". */
export function formatClock(d: Date, lang: Lang = "en"): string {
  return new Intl.DateTimeFormat(BCP47[lang], { hour: "2-digit", minute: "2-digit" }).format(d);
}

export function formatBytes(b: number): string {
  if (b < 1024 * 1024) return `${Math.round(b / 1024)} KB`;
  return `${(b / 1024 / 1024).toFixed(b < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}
