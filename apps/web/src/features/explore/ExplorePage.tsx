import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Castle, Droplets, Landmark, Mountain, PawPrint, Sailboat, Sun, TreePine, Waves } from "lucide-react";
import { api, type Destination } from "../../lib/api";
import { useT, type Key } from "../../lib/i18n";
import { regionCenter, useRegions } from "../../lib/queries";
import { useApp } from "../../state/store";
import { Chip, EmptyState, ErrorState, ICON, IconButton, ListRow, Skeleton } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";
import { useMapStore } from "../map/mapStore";
import { routeHref } from "../route/RoutePage";

const TYPES = ["waterfall", "fort", "lake", "temple", "hill", "wildlife", "heritage", "beach"] as const;
type DType = (typeof TYPES)[number] | "other";

const typeOf = (c: string): DType => ((TYPES as readonly string[]).includes(c) ? (c as DType) : "other");

const ICONS: Record<DType, React.ReactNode> = {
  waterfall: <Droplets {...ICON} />,
  fort: <Castle {...ICON} />,
  lake: <Waves {...ICON} />,
  temple: <Landmark {...ICON} />,
  hill: <Mountain {...ICON} />,
  wildlife: <PawPrint {...ICON} />,
  heritage: <Landmark {...ICON} />,
  beach: <Sun {...ICON} />,
  other: <TreePine {...ICON} />,
};

export default function ExplorePage() {
  const t = useT();
  const navigate = useNavigate();
  const { regionId } = useParams();
  const regions = useRegions();
  const { regionId: current, set } = useApp();
  const map = useMapStore((s) => s.map);
  const id = regionId ?? current;
  const region = regions.data?.regions.find((r) => r.id === id);
  const [type, setType] = useState<DType | "all">("all");

  const dests = useQuery({
    queryKey: ["destinations", id],
    enabled: !!region?.seeded,
    queryFn: () => api.destinations(id),
    staleTime: 60 * 60 * 1000,
  });

  const groups = useMemo(() => {
    const list = dests.data?.destinations ?? [];
    return list.filter((d: Destination) => type === "all" || typeOf(d.category) === type);
  }, [dests.data, type]);

  const typesPresent = useMemo(() => {
    const s = new Set((dests.data?.destinations ?? []).map((d) => typeOf(d.category)));
    return [...TYPES, "other" as const].filter((x) => s.has(x));
  }, [dests.data]);

  const pickRegion = (rid: string) => {
    set({ regionId: rid });
    const r = regions.data?.regions.find((x) => x.id === rid);
    if (r) map?.flyTo({ center: [regionCenter(r).lng, regionCenter(r).lat], zoom: 11 });
    navigate(`/explore/${rid}`, { replace: true });
  };

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate("/")}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{t("explore.title")}</h1>
    </div>
  );

  return (
    <Panel title={t("explore.title")} snap={0.92} header={header}>
      <SectionTitle>{t("explore.regions")}</SectionTitle>
      {regions.isPending ? (
        <div className="flex gap-2 px-4">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-12 w-24" />
          ))}
        </div>
      ) : regions.isError ? (
        <ErrorState message={(regions.error as Error).message} onRetry={() => regions.refetch()} retryLabel={t("common.retry")} />
      ) : (
        <div className="flex gap-2 overflow-x-auto px-4 pb-2" role="group" aria-label={t("explore.regions")}>
          {regions.data!.regions.map((r) => (
            <Chip key={r.id} pressed={r.id === id} onClick={() => pickRegion(r.id)}>
              {r.name.replace(/ \(.*\)$/, "")}
              {!r.seeded && <span className="text-ink-3"> · {t("explore.not_ready")}</span>}
            </Chip>
          ))}
        </div>
      )}

      {region && !region.seeded ? (
        <EmptyState icon={<Sailboat {...ICON} />} title={t("explore.not_ready")} body={region.name} />
      ) : (
        <>
          {typesPresent.length > 1 && (
            <div className="flex gap-2 overflow-x-auto px-4 pt-2 pb-2">
              <Chip pressed={type === "all"} onClick={() => setType("all")}>
                {t("explore.all")}
              </Chip>
              {typesPresent.map((x) => (
                <Chip key={x} pressed={type === x} onClick={() => setType(x)}>
                  {t(`type.${x}` as Key)}
                </Chip>
              ))}
            </div>
          )}
          {dests.isPending ? (
            <div className="flex flex-col gap-2 px-4 pt-2">
              {[0, 1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-14" />
              ))}
            </div>
          ) : dests.isError ? (
            <ErrorState message={(dests.error as Error).message} onRetry={() => dests.refetch()} retryLabel={t("common.retry")} />
          ) : groups.length === 0 ? (
            <EmptyState title={t("search.no_results")} />
          ) : (
            <ul>
              {groups.map((d) => (
                <li key={d.id}>
                  <ListRow
                    icon={ICONS[typeOf(d.category)]}
                    title={d.name}
                    subtitle={
                      !d.verified
                        ? t("explore.unverified")
                        : d.nearby_shortcuts
                          ? t("explore.shortcuts_near", { n: d.nearby_shortcuts })
                          : t(`type.${typeOf(d.category)}` as Key)
                    }
                    trailing={d.approx_km ? <span className="num">{d.approx_km} km</span> : undefined}
                    onClick={d.verified && d.lat != null ? () => navigate(routeHref({ lat: d.lat!, lng: d.lng!, name: d.name })) : undefined}
                  />
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      <div className="h-6" />
    </Panel>
  );
}
