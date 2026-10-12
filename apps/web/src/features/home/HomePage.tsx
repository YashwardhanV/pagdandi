import { useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Bike, Compass, Crosshair, Mountain, Plus, Search, User, Route as RouteIcon } from "lucide-react";
import { api } from "../../lib/api";
import { fmtLatLng } from "../../lib/geo";
import { locateOnce } from "../../lib/geolocation";
import { useT } from "../../lib/i18n";
import { regionCenter, useRegion } from "../../lib/queries";
import { useApp } from "../../state/store";
import { Chip, EmptyState, ICON, IconButton, ListRow, Skeleton, toast } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";
import { confirmLocation } from "../../components/LocateExplainer";
import { useMapStore } from "../map/mapStore";
import { routeHref } from "../route/RoutePage";
import { CandidatePrompt } from "./CandidatePrompt";

export default function HomePage() {
  const t = useT();
  const navigate = useNavigate();
  const region = useRegion();
  const { userPos, recents, vehicle, set } = useApp();
  const map = useMapStore((s) => s.map);

  const center = userPos ?? (region ? regionCenter(region) : null);
  const nearby = useQuery({
    queryKey: ["nearby", center && fmtLatLng({ lat: +center.lat.toFixed(2), lng: +center.lng.toFixed(2) })],
    enabled: !!center && !!region?.seeded,
    queryFn: () => api.shortcuts([center!.lng - 0.08, center!.lat - 0.08, center!.lng + 0.08, center!.lat + 0.08], 13),
    staleTime: 60_000,
  });
  const dests = useQuery({
    queryKey: ["destinations", region?.id],
    enabled: !!region?.seeded,
    queryFn: () => api.destinations(region!.id),
    staleTime: 60 * 60 * 1000,
  });

  const locate = async () => {
    if (!(await confirmLocation())) return;
    try {
      const p = await locateOnce();
      map?.easeTo({ center: [p.lng, p.lat], zoom: 15 });
    } catch {
      toast(t("home.locate_denied"));
    }
  };

  const vehicles = region?.vehicles ?? ["scooter", "motorcycle"];
  const cur = vehicle && vehicles.includes(vehicle) ? vehicle : (region?.default_vehicle ?? "scooter");
  const cycleVehicle = () => {
    const i = vehicles.indexOf(cur);
    const next = vehicles[(i + 1) % vehicles.length]!;
    set({ vehicle: next });
    toast(t(`vehicle.${next}`));
  };

  const n = nearby.data?.shortcuts.filter((s) => s.status === "active").length ?? 0;

  return (
    <>
      {/* Top: search pill + vehicle, then recent chips (SPEC §10.2.1). */}
      <div className="pointer-events-none fixed inset-x-0 top-0 z-10 flex flex-col gap-2 p-3 pt-[max(32px,calc(env(safe-area-inset-top)+24px))] lg:left-[400px] lg:max-w-lg">
        <div className="pointer-events-auto flex items-center gap-2 rounded-full bg-surface p-1 pl-4 shadow-float">
          <Search size={18} strokeWidth={1.75} className="text-ink-3" aria-hidden />
          <button type="button" className="h-12 flex-1 text-left text-base text-ink-2" onClick={() => navigate("/search")} data-testid="where-to">
            {t("home.where_to")}
          </button>
          <IconButton label={`${t("vehicle.choose")}: ${t(`vehicle.${cur}`)}`} onClick={cycleVehicle}>
            <Bike {...ICON} />
          </IconButton>
        </div>
        {recents.length > 0 && (
          <div className="pointer-events-auto flex gap-2 overflow-x-auto pb-1">
            {recents.slice(0, 4).map((r) => (
              <Chip key={fmtLatLng(r)} onClick={() => navigate(routeHref(r))} className="shadow-float">
                {r.name}
              </Chip>
            ))}
          </div>
        )}
      </div>

      {/* Locate: bottom-right, above the collapsed sheet. */}
      <div className="fixed right-3 bottom-[calc(18%+12px)] z-10 flex flex-col gap-2 lg:bottom-6">
        <IconButton floating label={t("home.add_shortcut")} onClick={() => navigate("/add")}>
          <Plus {...ICON} />
        </IconButton>
        <IconButton floating label={t("home.locate")} onClick={locate}>
          <Crosshair {...ICON} />
        </IconButton>
      </div>

      <Panel title={t("app.name")} snap={0.18}>
        <div className="flex items-center justify-between px-4 pt-1 pb-2">
          <div className="flex items-center gap-2">
            <RouteIcon size={18} strokeWidth={1.75} className="text-accent" aria-hidden />
            {nearby.isPending && region?.seeded ? (
              <Skeleton className="h-5 w-40" />
            ) : (
              <span className="text-sm font-medium text-ink">{n > 0 ? t("home.nearby_shortcuts", { n }) : t("home.nearby_none")}</span>
            )}
          </div>
          <div className="flex">
            <IconButton label={t("home.explore")} onClick={() => navigate("/explore")}>
              <Compass {...ICON} />
            </IconButton>
            <IconButton label={t("home.profile")} onClick={() => navigate("/me")}>
              <User {...ICON} />
            </IconButton>
          </div>
        </div>
        <CandidatePrompt center={center} />
        <SectionTitle>{t("home.top_destinations")}</SectionTitle>
        {dests.isPending && region?.seeded ? (
          <div className="flex flex-col gap-2 px-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </div>
        ) : !region?.seeded ? (
          <EmptyState title={t("explore.not_ready")} />
        ) : (
          (dests.data?.destinations ?? [])
            .filter((d) => d.verified && d.lat != null)
            .slice(0, 8)
            .map((d) => (
              <ListRow
                key={d.id}
                icon={<Mountain {...ICON} />}
                title={d.name}
                subtitle={d.nearby_shortcuts ? t("explore.shortcuts_near", { n: d.nearby_shortcuts }) : undefined}
                trailing={d.approx_km ? <span className="num">{d.approx_km} km</span> : undefined}
                onClick={() => navigate(routeHref({ lat: d.lat!, lng: d.lng!, name: d.name }))}
              />
            ))
        )}
        <div className="h-4" />
      </Panel>
    </>
  );
}
