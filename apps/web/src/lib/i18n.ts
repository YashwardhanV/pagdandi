import en from "../i18n/en.json";
import hi from "../i18n/hi.json";
import sv from "../i18n/sv.json";
import { useApp, type Lang } from "../state/store";

export type Key = keyof typeof en;
type Dict = Record<string, string>;
const dicts: Record<Lang, Dict> = { en, hi, sv };

export const LANGS: { id: Lang; label: string }[] = [
  { id: "en", label: "English" },
  { id: "hi", label: "हिन्दी" },
  { id: "sv", label: "Svenska" },
];

/** translate looks up a key (falling back to English) and fills {placeholders}. */
export function translate(lang: Lang, key: Key, vars?: Record<string, string | number>): string {
  const s = dicts[lang][key] ?? (en as Dict)[key] ?? key;
  if (!vars) return s;
  return s.replace(/\{(\w+)\}/g, (_, k: string) => (vars[k] !== undefined ? String(vars[k]) : `{${k}}`));
}

export type T = (key: Key, vars?: Record<string, string | number>) => string;

/** useT returns a translator bound to the current language. */
export function useT(): T {
  const lang = useApp((s) => s.lang);
  return (key, vars) => translate(lang, key, vars);
}

/** The local word for unpaved road: "Kaccha road" in India, "Grusväg" in Sweden (SPEC §4.3). */
export function unpavedKey(country: string | undefined): Key {
  return country === "SE" ? "surface.unpaved.SE" : "surface.unpaved.IN";
}

export const BCP47: Record<Lang, string> = { en: "en-IN", hi: "hi-IN", sv: "sv-SE" };
