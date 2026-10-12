import { useState } from "react";
import { CloudRain, DoorClosed, Moon, Mountain, ShieldQuestion, Waves, Lightbulb, MoveHorizontal, PawPrint, Route as RouteIcon } from "lucide-react";
import type { RouteOption, Warning } from "../../lib/api";
import { formatAgo, formatDistance } from "../../lib/format";
import { unpavedKey, useT } from "../../lib/i18n";
import { useApp } from "../../state/store";
import { Chip, cx } from "../../components/ui";

const SMALL = { size: 14, strokeWidth: 1.75 } as const;

/** 6 px surface proportions bar: paved / kaccha / track (SPEC §11.4). Tap for a legend. */
export function SurfaceBar({ breakdown, country }: { breakdown: RouteOption["surface_breakdown"]; country?: string }) {
  const t = useT();
  const lang = useApp((s) => s.lang);
  const [legend, setLegend] = useState(false);
  const total = breakdown.paved_m + breakdown.unpaved_m + breakdown.track_m || 1;
  const parts = [
    { key: "paved", m: breakdown.paved_m, cls: "bg-ink-3 opacity-40", label: t("surface.paved") },
    { key: "unpaved", m: breakdown.unpaved_m, cls: "bg-warn", label: t(unpavedKey(country)) },
    { key: "track", m: breakdown.track_m, cls: "bg-danger opacity-70", label: t("surface.track") },
  ].filter((p) => p.m > 0);
  const summary = parts.map((p) => `${p.label} ${formatDistance(p.m, lang)}`).join(", ");
  return (
    <div>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setLegend((v) => !v);
        }}
        className="flex h-6 w-full items-center"
        aria-label={`${t("surface.legend")}: ${summary}`}
        aria-expanded={legend}
      >
        <span className="flex h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
          {parts.map((p) => (
            <span key={p.key} className={p.cls} style={{ width: `${(p.m / total) * 100}%` }} />
          ))}
        </span>
      </button>
      {legend && (
        <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-ink-2">
          {parts.map((p) => (
            <li key={p.key} className="flex items-center gap-1.5">
              <span className={cx("inline-block h-1.5 w-3 rounded-full", p.cls)} aria-hidden />
              {p.label} <span className="num">{formatDistance(p.m, lang)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function bandOf(c: number): "high" | "medium" | "low" {
  return c >= 0.75 ? "high" : c >= 0.5 ? "medium" : "low";
}

/** Dot plus "High · 3 days ago", neutral background, colour from the band (SPEC §11.4). */
export function ConfidenceChip({ confidence, lastConfirmed, riders }: { confidence: number; lastConfirmed?: string | null; riders?: number }) {
  const t = useT();
  const lang = useApp((s) => s.lang);
  const band = bandOf(confidence);
  const tone = band === "high" ? "ok" : band === "medium" ? "warn" : "danger";
  const ago = formatAgo(lastConfirmed, lang);
  let text = t(`confidence.${band}`);
  if (ago && riders && riders > 1) text += ` · ${t("confidence.by_riders", { ago, n: riders })}`;
  else if (ago && riders === 1) text += ` · ${t("confidence.by_one", { ago })}`;
  else if (ago) text = t("confidence.ago", { band: text, ago });
  return (
    <Chip tone={tone} icon={<span className="inline-block h-2 w-2 rounded-full bg-current" aria-hidden />}>
      <span className="text-ink-2">{text}</span>
    </Chip>
  );
}

/** Warning chips: Gate, Kaccha 1.2 km, Night, … (SPEC §10.2). */
export function WarningChips({ warnings, country, max = 4 }: { warnings: Warning[]; country?: string; max?: number }) {
  const t = useT();
  const lang = useApp((s) => s.lang);
  const seen = new Set<string>();
  const chips: { key: string; label: string; icon: React.ReactNode; tone: "warn" | "danger" | "neutral" }[] = [];
  for (const w of warnings) {
    const k = w.type === "unpaved" ? `unpaved-${w.at_m}` : w.type;
    if (seen.has(k)) continue;
    seen.add(k);
    switch (w.type) {
      case "unpaved":
        chips.push({
          key: k,
          label: t("warn.unpaved", { surface: w.surface === "track" ? t("surface.track") : t(unpavedKey(country)), len: formatDistance(w.length_m ?? 0, lang) }),
          icon: <RouteIcon {...SMALL} aria-hidden />,
          tone: "warn",
        });
        break;
      case "gate":
        chips.push({ key: k, label: t("warn.gate"), icon: <DoorClosed {...SMALL} aria-hidden />, tone: "warn" });
        break;
      case "night":
        chips.push({ key: k, label: t("warn.night"), icon: <Moon {...SMALL} aria-hidden />, tone: "neutral" });
        break;
      case "seasonal":
        chips.push({ key: k, label: w.season ? cap(w.season.replace("_", " ")) : t("warn.seasonal", { season: "" }), icon: <CloudRain {...SMALL} aria-hidden />, tone: "warn" });
        break;
      case "steep":
        chips.push({ key: k, label: t("warn.steep"), icon: <Mountain {...SMALL} aria-hidden />, tone: "warn" });
        break;
      case "low_confidence":
        chips.push({ key: k, label: t("warn.low_confidence"), icon: <ShieldQuestion {...SMALL} aria-hidden />, tone: "danger" });
        break;
      case "floods":
        chips.push({ key: k, label: t("warn.floods"), icon: <Waves {...SMALL} aria-hidden />, tone: "warn" });
        break;
      case "narrow":
        chips.push({ key: k, label: t("warn.narrow"), icon: <MoveHorizontal {...SMALL} aria-hidden />, tone: "neutral" });
        break;
      case "livestock":
        chips.push({ key: k, label: t("warn.livestock"), icon: <PawPrint {...SMALL} aria-hidden />, tone: "neutral" });
        break;
      case "no_lights":
        chips.push({ key: k, label: t("warn.no_lights"), icon: <Lightbulb {...SMALL} aria-hidden />, tone: "neutral" });
        break;
    }
  }
  if (!chips.length) return null;
  return (
    <ul className="flex flex-wrap gap-1.5" aria-label="Warnings">
      {chips.slice(0, max).map((c) => (
        <li key={c.key}>
          <Chip tone={c.tone} icon={c.icon}>
            {c.label}
          </Chip>
        </li>
      ))}
    </ul>
  );
}

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
