import { useEffect, useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Bike, Download, ExternalLink, Navigation, ArrowUpDown, CircleDot, MapPin } from "lucide-react";
import { ApiError, api, type Mode, type Vehicle } from "../../lib/api";
import { fmtLatLng, parseLatLng } from "../../lib/geo";
import { locateOnce } from "../../lib/geolocation";
import { useT } from "../../lib/i18n";
import { useOnline } from "../../lib/hooks";
import { useRegion } from "../../lib/queries";
import { rememberRoute, useApp } from "../../state/store";
import { Button, Chip, EmptyState, ErrorState, ICON, IconButton, SegmentedControl } from "../../components/ui";
import { Panel } from "../../components/ui/Sheet";
import { confirmLocation } from "../../components/LocateExplainer";
import { RouteCompareCard, RouteCompareSkeleton } from "./RouteCompareCard";

export function routeHref(to: { lat: number; lng: number; name: string }, from?: { lat: number; lng: number; name: string } | null) {
  const p = new URLSearchParams({ to: fmtLatLng(to), toName: to.name });
  if (from) {
    p.set("from", fmtLatLng(from));
    p.set("fromName", from.name);
  }
  return `/route?${p}`;
}

export default function RoutePage() {
  const t = useT();
  const navigate = useNavigate();
  const online = useOnline();
  const [params, setParams] = useSearchParams();
  const region = useRegion();
  const { lang, mode, vehicle, userPos, selected, geoError, set } = useApp();

  const to = parseLatLng(params.get("to"));
  const fromParam = parseLatLng(params.get("from"));
  const toName = params.get("toName") ?? "";
  const fromName = params.get("fromName") ?? t("search.your_location");

  // Starting from "my location" needs a fix; ask only now that the user asked for a route.
  useEffect(() => {
    if (!fromParam && !userPos) {
      confirmLocation().then((ok) => {
        if (ok) locateOnce().catch(() => {});
      });
    }
  }, [fromParam, userPos]);

  const from = fromParam ?? (userPos ? { lat: userPos.lat, lng: userPos.lng } : null);
  const effVehicle: Vehicle = vehicle && region?.vehicles.includes(vehicle) ? vehicle : (region?.default_vehicle ?? "scooter");

  const q = useQuery({
    queryKey: ["route", from && fmtLatLng(from), to && fmtLatLng(to), effVehicle, mode, lang],
    enabled: !!from && !!to && online,
    queryFn: ({ signal }) => api.route({ from: from!, to: to!, vehicle: effVehicle, mode, lang }, signal),
    staleTime: 5 * 60 * 1000,
    retry: (n, e) => n < 1 && (e as ApiError).status >= 500,
  });

  useEffect(() => {
    if (q.data) {
      set({ route: q.data, selected: q.data.recommended });
      rememberRoute(q.data, fromParam ? { ...fromParam, name: fromName } : null, to ? { ...to, name: toName } : null);
    }
  }, [q.data]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => set({ route: null, selected: null }), [set]);

  const data = q.data;
  const sel = selected ?? data?.recommended ?? "std";
  const country = region?.country;
  const selOpt = data?.options.find((o) => o.id === sel);

  const googleUrl = useMemo(() => {
    if (!to) return "";
    const p = new URLSearchParams({ api: "1", destination: fmtLatLng(to), travelmode: country === "SE" ? "bicycling" : "two-wheeler" });
    if (fromParam) p.set("origin", fmtLatLng(fromParam));
    return `https://www.google.com/maps/dir/?${p}`;
  }, [to, fromParam, country]);

  const swap = () => {
    if (!to) return;
    const next = new URLSearchParams();
    next.set("from", fmtLatLng(to));
    next.set("fromName", toName);
    const f = fromParam ?? (userPos ? { lat: userPos.lat, lng: userPos.lng } : null);
    if (f) {
      next.set("to", fmtLatLng(f));
      next.set("toName", fromParam ? fromName : t("search.your_location"));
    }
    setParams(next, { replace: true });
  };

  const err = q.error as ApiError | null;

  const footer = data ? (
        <div className="flex flex-col gap-1.5 px-4 pt-2 pb-3">
          {!data.context.daylight && <p className="text-xs text-ink-2">{t("route.night_note")}</p>}
          {data.context.season && <p className="text-xs text-ink-2">{t("route.season_note", { season: data.context.season.replace("_", " ") })}</p>}
          {selOpt?.kind === "shortcut" && data.disclaimer && <p className="text-xs text-ink-2">{t("route.disclaimer")}</p>}
          <div className="flex items-center gap-2">
            <Button variant="primary" className="flex-1" icon={<Navigation {...ICON} aria-hidden />} onClick={() => navigate(`/nav/${data.request_id}/${sel}`)} data-testid="ride">
              {t("route.ride")}
            </Button>
            <a
              className="inline-flex h-12 w-12 items-center justify-center rounded-full text-ink-2 hover:bg-surface-2"
              href={api.gpxUrl(data.request_id, sel)}
              download
              aria-label={t("route.gpx")}
              title={t("route.gpx")}
            >
              <Download {...ICON} aria-hidden />
            </a>
            <a
              className="inline-flex h-12 w-12 items-center justify-center rounded-full text-ink-2 hover:bg-surface-2"
              href={googleUrl}
              target="_blank"
              rel="noreferrer noopener"
              aria-label={t("route.google")}
              title={t("route.google")}
            >
              <ExternalLink {...ICON} aria-hidden />
            </a>
          </div>
        </div>
      ) : undefined;

  const header = (
    <div className="flex items-start gap-1 border-b border-line px-2 pt-1 pb-3">
      <IconButton label={t("common.back")} onClick={() => navigate("/")}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <div className="min-w-0 flex-1 pt-1">
        <button type="button" onClick={() => navigate(`/search?field=from&${params}`)} className="flex min-h-10 w-full items-center gap-2 text-left">
          <CircleDot size={16} strokeWidth={1.75} className="shrink-0 text-ink-3" aria-hidden />
          <span className="sr-only">{t("search.from")}:</span>
          <span className="truncate text-sm text-ink-2">{fromName}</span>
        </button>
        <button type="button" onClick={() => navigate(`/search?field=to&${params}`)} className="flex min-h-10 w-full items-center gap-2 text-left">
          <MapPin size={16} strokeWidth={1.75} className="shrink-0 text-accent" aria-hidden />
          <span className="sr-only">{t("search.to")}:</span>
          <span className="truncate text-base font-medium text-ink">{toName}</span>
        </button>
      </div>
      <IconButton label={t("search.swap")} onClick={swap}>
        <ArrowUpDown {...ICON} />
      </IconButton>
    </div>
  );

  return (
    <Panel title={toName || t("route.options")} snap={0.52} header={header} footer={footer}>
      <div className="min-h-[264px]">
        {!online ? (
          <EmptyState title={t("route.offline")} />
        ) : !from ? (
          <EmptyState title={t("nav.gps_waiting")} body={geoError === "denied" ? t("home.locate_denied") : undefined} />
        ) : q.isPending ? (
          <RouteCompareSkeleton />
        ) : err ? (
          <RouteError err={err} onRetry={() => q.refetch()} />
        ) : data ? (
          <>
            <RouteCompareCard options={data.options} recommended={data.recommended} selected={sel} onSelect={(id) => set({ selected: id })} country={country} />
            {data.options.length === 1 && mode !== "standard" && <p className="px-4 py-3 text-sm text-ink-2">{t("route.no_shortcut")}</p>}
          </>
        ) : null}
      </div>

      <div className="flex flex-col gap-3 border-t border-line px-4 pt-3 pb-2">
        <SegmentedControl<Mode>
          label="Mode"
          value={mode}
          onChange={(m) => set({ mode: m })}
          options={(["auto", "standard", "shortcut", "adventure"] as Mode[]).map((m) => ({ value: m, label: t(`mode.${m}`) }))}
        />
        {region && region.vehicles.length > 1 && (
          <div className="flex gap-2" role="group" aria-label={t("vehicle.choose")}>
            {region.vehicles.map((v) => (
              <Chip key={v} onClick={() => set({ vehicle: v })} pressed={v === effVehicle} icon={<Bike size={16} strokeWidth={1.75} aria-hidden />}>
                {t(`vehicle.${v}`)}
              </Chip>
            ))}
          </div>
        )}
        {mode === "adventure" && <p className="text-xs text-ink-2">{t("mode.adventure_hint")}</p>}
      </div>

    </Panel>
  );
}

function RouteError({ err, onRetry }: { err: ApiError; onRetry: () => void }) {
  const t = useT();
  if (err.code === "OUT_OF_COVERAGE") {
    const d = err.details as { nearest_region_name?: string; distance_km?: number } | undefined;
    return (
      <EmptyState
        title={t("route.out_of_coverage")}
        body={d?.nearest_region_name ? t("route.nearest_region", { name: d.nearest_region_name, km: Math.round(d.distance_km ?? 0) }) : undefined}
      />
    );
  }
  if (err.code === "NO_ROUTE") return <EmptyState title={t("route.no_route")} />;
  return <ErrorState message={err.message || t("common.error")} onRetry={onRetry} retryLabel={t("common.retry")} />;
}
