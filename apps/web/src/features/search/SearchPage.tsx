import { useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Briefcase, Clock, Crosshair, Home, MapPin, Mountain, Search, X } from "lucide-react";
import { api, type Place } from "../../lib/api";
import { fmtLatLng } from "../../lib/geo";
import { useDebounced } from "../../lib/hooks";
import { useT } from "../../lib/i18n";
import { regionCenter, useRegion } from "../../lib/queries";
import { useApp, type Endpoint } from "../../state/store";
import { EmptyState, ErrorState, ICON, IconButton, ListRow, Skeleton } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";

export default function SearchPage() {
  const t = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const rawField = params.get("field");
  const field = rawField === "from" || rawField === "home" || rawField === "work" ? rawField : "to";
  const region = useRegion();
  const { lang, userPos, recents, home, work, addRecent } = useApp();
  const [q, setQ] = useState("");
  const dq = useDebounced(q.trim(), 250);
  const input = useRef<HTMLInputElement>(null);

  const near = userPos ?? (region ? regionCenter(region) : undefined);
  const results = useQuery({
    queryKey: ["geocode", dq, near && fmtLatLng(near), lang],
    enabled: dq.length >= 2,
    queryFn: () => api.geocode(dq, near, lang),
    staleTime: 10 * 60 * 1000,
  });
  const dests = useQuery({
    queryKey: ["destinations", region?.id],
    enabled: !!region?.seeded,
    queryFn: () => api.destinations(region!.id),
    staleTime: 60 * 60 * 1000,
  });

  const pick = (e: Endpoint) => {
    if (field === "home" || field === "work") {
      // Saved places also define the 300 m privacy cut for ride traces (SPEC §17).
      useApp.getState().set({ [field]: e });
      api.putPlace(field, e.lat, e.lng).catch(() => {});
      navigate("/me", { replace: true });
      return;
    }
    addRecent(e);
    const next = new URLSearchParams(params);
    next.delete("field");
    if (field === "from") {
      next.set("from", fmtLatLng(e));
      next.set("fromName", e.name);
      if (!next.get("to")) {
        navigate(`/search?field=to&${next}`, { replace: true });
        return;
      }
    } else {
      next.set("to", fmtLatLng(e));
      next.set("toName", e.name);
    }
    navigate(`/route?${next}`);
  };

  const pickMyLocation = () => {
    const next = new URLSearchParams(params);
    next.delete("field");
    next.delete("from");
    next.delete("fromName");
    navigate(next.get("to") ? `/route?${next}` : "/");
  };

  const curated = useMemo(() => {
    const list = (dests.data?.destinations ?? []).filter((d) => d.verified && d.lat != null && d.lng != null);
    if (!dq) return list.slice(0, 8);
    const low = dq.toLowerCase();
    return list.filter((d) => d.name.toLowerCase().includes(low)).slice(0, 5);
  }, [dests.data, dq]);

  const places: Place[] = (results.data?.results ?? []).filter((p) => p.source !== "destination" || !curated.some((c) => c.name === p.name));
  const savedList = [home && { kind: "home" as const, e: home }, work && { kind: "work" as const, e: work }].filter(Boolean) as { kind: "home" | "work"; e: Endpoint }[];

  const header = (
    <div className="flex items-center gap-1 border-b border-line px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate(-1)}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <label className="flex h-12 flex-1 items-center gap-2 rounded-md bg-surface-2 px-3">
        <Search size={18} strokeWidth={1.75} className="text-ink-3" aria-hidden />
        <span className="sr-only">{field === "from" ? t("search.from") : field === "to" ? t("search.to") : t(`search.set_${field}`)}</span>
        <input
          ref={input}
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={field === "from" ? t("search.from") : field === "home" ? t("search.set_home") : field === "work" ? t("search.set_work") : t("home.where_to")}
          className="h-full min-w-0 flex-1 bg-transparent text-base text-ink outline-none placeholder:text-ink-3"
          enterKeyHint="search"
          autoComplete="off"
          data-testid="search-input"
        />
        {q && (
          <button type="button" className="flex h-10 w-10 items-center justify-center text-ink-3" aria-label={t("common.close")} onClick={() => setQ("")}>
            <X size={18} strokeWidth={1.75} />
          </button>
        )}
      </label>
    </div>
  );

  return (
    <Panel title={t("search.placeholder")} snap={0.92} header={header}>
      {field === "from" && <ListRow icon={<Crosshair {...ICON} />} title={t("search.your_location")} onClick={pickMyLocation} />}

      {!dq && savedList.length > 0 && (
        <section>
          <SectionTitle>{t("search.saved")}</SectionTitle>
          {savedList.map(({ kind, e }) => (
            <ListRow key={kind} icon={kind === "home" ? <Home {...ICON} /> : <Briefcase {...ICON} />} title={t(`search.${kind}`)} subtitle={e.name} onClick={() => pick(e)} />
          ))}
        </section>
      )}

      {!dq && recents.length > 0 && (
        <section>
          <SectionTitle>{t("search.recent")}</SectionTitle>
          {recents.slice(0, 5).map((r) => (
            <ListRow key={fmtLatLng(r)} icon={<Clock {...ICON} />} title={r.name} subtitle={r.label} onClick={() => pick(r)} />
          ))}
        </section>
      )}

      {curated.length > 0 && (
        <section>
          <SectionTitle>{t("search.destinations")}</SectionTitle>
          {curated.map((d) => (
            <ListRow
              key={d.id}
              icon={<Mountain {...ICON} />}
              title={d.name}
              subtitle={t(`type.${d.category}` as never) !== `type.${d.category}` ? t(`type.${d.category}` as never) : d.category}
              trailing={d.approx_km ? <span className="num">{d.approx_km} km</span> : undefined}
              onClick={() => pick({ lat: d.lat!, lng: d.lng!, name: d.name, label: region?.name })}
            />
          ))}
        </section>
      )}

      {dq.length >= 2 && (
        <section>
          <SectionTitle>{t("search.places")}</SectionTitle>
          {results.isPending ? (
            <div className="flex flex-col gap-2 px-4">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-12" />
              ))}
            </div>
          ) : results.isError ? (
            <ErrorState message={(results.error as Error).message} onRetry={() => results.refetch()} retryLabel={t("common.retry")} />
          ) : places.length === 0 && curated.length === 0 ? (
            <EmptyState title={t("search.no_results")} />
          ) : (
            places.map((p) => (
              <ListRow
                key={p.id}
                icon={<MapPin {...ICON} />}
                title={p.name}
                subtitle={p.label}
                trailing={!p.in_coverage ? <span className="text-xs text-ink-3">{t("search.outside")}</span> : undefined}
                onClick={() => pick({ lat: p.lat, lng: p.lng, name: p.name, label: p.label })}
              />
            ))
          )}
        </section>
      )}
      {dq.length === 1 && <p className="px-4 py-3 text-sm text-ink-3">{t("search.type_more")}</p>}
      <div className="h-6" />
    </Panel>
  );
}
