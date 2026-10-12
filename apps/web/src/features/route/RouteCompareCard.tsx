import type { RouteOption } from "../../lib/api";
import { formatDistance, formatDuration, minutes } from "../../lib/format";
import { useT } from "../../lib/i18n";
import { useApp } from "../../state/store";
import { cx } from "../../components/ui";
import { ConfidenceChip, SurfaceBar, WarningChips } from "./parts";

/**
 * The core comparison: one row per option, selected row marked by a 2 px left
 * border in the route's colour; big tabular numbers (SPEC §10.2.3, §11.4).
 */
export function RouteCompareCard({
  options,
  recommended,
  selected,
  onSelect,
  country,
}: {
  options: RouteOption[];
  recommended: string;
  selected: string;
  onSelect: (id: string) => void;
  country?: string;
}) {
  const t = useT();
  const lang = useApp((s) => s.lang);
  const shortcuts = options.filter((o) => o.kind === "shortcut");
  return (
    <div role="radiogroup" aria-label={t("route.options")} className="divide-y divide-line">
      {options.map((o) => {
        const isSel = o.id === selected;
        const isStd = o.kind === "standard";
        const name = isStd ? t("route.usual_way") : shortcuts.length > 1 ? `${t("route.pagdandi")} ${shortcuts.indexOf(o) + 1}` : t("route.pagdandi");
        const community = o.legs.some((l) => l.source === "community");
        const saved = o.savings?.duration_s ?? 0;
        return (
          <div
            key={o.id}
            data-testid={`row-${o.id}`}
            onClick={() => onSelect(o.id)}
            className={cx(
              "flex cursor-pointer flex-col gap-2 border-l-2 px-4 py-3 hover:bg-surface-2",
              isSel ? (isStd ? "border-route-standard" : "border-accent") : "border-transparent",
            )}
          >
            <button
              type="button"
              role="radio"
              aria-checked={isSel}
              data-testid={`option-${o.id}`}
              onClick={(e) => {
                e.stopPropagation();
                onSelect(o.id);
              }}
              className="flex flex-col gap-2 text-left outline-none focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
            >
              <span className="flex items-center gap-2">
                <span className={cx("h-2 w-2 rounded-full", isStd ? "bg-route-standard" : "bg-accent")} aria-hidden />
                <span className="text-sm font-medium text-ink">{name}</span>
                {o.id === recommended && (
                  <span className="rounded-sm bg-accent px-1.5 text-xs font-medium text-accent-ink">{t("route.recommended")}</span>
                )}
              </span>
              <span className="flex items-baseline gap-3">
                <span className="num text-xl text-ink">{formatDuration(o.duration_s, lang)}</span>
                <span className="num text-sm text-ink-2">{formatDistance(o.distance_m, lang)}</span>
                {!isStd && saved >= 60 && <span className="text-sm font-medium text-ok">{t("route.saves_min", { n: minutes(saved) })}</span>}
                {!isStd && saved <= -60 && <span className="text-sm text-ink-2">{t("route.slower_min", { n: minutes(-saved) })}</span>}
              </span>
            </button>
            <SurfaceBar breakdown={o.surface_breakdown} country={country} />
            <div className="flex flex-wrap items-center gap-1.5">
              {community && o.confidence != null && (
                <ConfidenceChip confidence={o.confidence} lastConfirmed={o.last_confirmed_at} riders={o.confirmations_30d} />
              )}
              {!isStd && !community && <span className="text-xs text-ink-2">{t("route.osm_only")}</span>}
            </div>
            <WarningChips warnings={o.warnings} country={country} />
          </div>
        );
      })}
    </div>
  );
}

/** Reserved-height skeleton so the sheet does not jump when results arrive. */
export function RouteCompareSkeleton() {
  return (
    <div className="divide-y divide-line" aria-busy>
      {[0, 1].map((i) => (
        <div key={i} className="flex h-[132px] flex-col gap-2 px-4 py-3">
          <div className="skeleton h-4 w-24" />
          <div className="skeleton h-7 w-40" />
          <div className="skeleton h-1.5 w-full" />
          <div className="skeleton h-6 w-48" />
        </div>
      ))}
    </div>
  );
}
