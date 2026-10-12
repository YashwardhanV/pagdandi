import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, ExternalLink, Navigation, X, Minus } from "lucide-react";
import { ApiError, api, type Flag } from "../../lib/api";
import { bounds, decodePolyline } from "../../lib/geo";
import { formatAgo, formatDistance } from "../../lib/format";
import { unpavedKey, useT } from "../../lib/i18n";
import { useRegion } from "../../lib/queries";
import { useApp } from "../../state/store";
import { Button, Chip, ErrorState, ICON, IconButton, Skeleton, toast } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";
import { useMapStore } from "../map/mapStore";
import { ConfidenceChip } from "../route/parts";
import { routeHref } from "../route/RoutePage";

const REASONS = ["gate_locked", "flooded", "construction", "private", "unsafe", "missing", "other"] as const;

/** 90-day confidence sparkline, drawn in currentColor so it follows the theme. */
export function Sparkline({ points, label }: { points: { day: string; confidence: number }[]; label: string }) {
  if (points.length < 2) return null;
  const w = 320;
  const h = 48;
  const xs = (i: number) => (i / (points.length - 1)) * w;
  const ys = (c: number) => h - 4 - c * (h - 8);
  const d = points.map((p, i) => `${i ? "L" : "M"}${xs(i).toFixed(1)},${ys(p.confidence).toFixed(1)}`).join(" ");
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="h-12 w-full text-accent" role="img" aria-label={label} preserveAspectRatio="none">
      <line x1="0" x2={w} y1={ys(0.75)} y2={ys(0.75)} className="stroke-line" strokeDasharray="3 3" />
      <line x1="0" x2={w} y1={ys(0.5)} y2={ys(0.5)} className="stroke-line" strokeDasharray="3 3" />
      <path d={d} fill="none" stroke="currentColor" strokeWidth="2" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

export default function ShortcutDetail() {
  const t = useT();
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const lang = useApp((s) => s.lang);
  const region = useRegion();
  const { set: setMap, fitTo } = useMapStore();
  const [blocking, setBlocking] = useState(false);

  const q = useQuery({ queryKey: ["shortcut", id], queryFn: () => api.shortcut(id) });
  const sc = q.data?.shortcut;

  useEffect(() => {
    if (!sc) return;
    setMap({ highlight: { polyline: sc.polyline, kind: "shortcut" } });
    try {
      fitTo(bounds(decodePolyline(sc.polyline)), 80);
    } catch {
      /* ignore */
    }
    return () => setMap({ highlight: null });
  }, [sc, setMap, fitTo]);

  const report = useMutation({
    mutationFn: (body: { verdict: "passable" | "blocked" | "partly"; reason?: (typeof REASONS)[number] }) => api.report(id, body),
    onSuccess: (r) => {
      toast(t("shortcut.reported", { band: t(`confidence.${r.band}`) }));
      setBlocking(false);
      void qc.invalidateQueries({ queryKey: ["shortcut", id] });
      void qc.invalidateQueries({ queryKey: ["shortcuts-view"] });
    },
    onError: (e: ApiError) => {
      if (e.status === 401) navigate(`/signin?redirect=/s/${id}`);
      else toast(e.code === "ALREADY_REPORTED" ? t("shortcut.already") : e.message);
    },
  });

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate(-1)}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="min-w-0 flex-1 truncate text-lg font-semibold text-ink">{sc?.title ?? t("shortcut.title_fallback")}</h1>
    </div>
  );

  return (
    <Panel title={sc?.title ?? t("shortcut.title_fallback")} snap={0.52} header={header}>
      {q.isPending ? (
        <div className="flex flex-col gap-3 px-4">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-12" />
          <Skeleton className="h-24" />
        </div>
      ) : q.isError ? (
        <ErrorState message={(q.error as Error).message} onRetry={() => q.refetch()} retryLabel={t("common.retry")} />
      ) : sc && q.data ? (
        <div className="flex flex-col gap-1 pb-6">
          <div className="flex flex-wrap items-center gap-1.5 px-4">
            <Chip tone={sc.status === "active" ? "ok" : sc.status === "blocked" ? "danger" : "neutral"}>{t(`shortcut.status.${sc.status}`)}</Chip>
            <Chip>{sc.surface === "paved" ? t("surface.paved") : sc.surface === "track" ? t("surface.track") : t(unpavedKey(region?.country))}</Chip>
            <Chip>
              <span className="num">{formatDistance(sc.length_m, lang)}</span>
            </Chip>
            <ConfidenceChip confidence={sc.confidence} lastConfirmed={sc.last_confirmed_at} riders={q.data.confirmations_30d} />
          </div>
          {sc.flags.length > 0 && (
            <div className="flex flex-wrap gap-1.5 px-4 pt-2">
              {sc.flags.map((f: Flag) => (
                <Chip key={f} tone="warn">
                  {t(`flag.${f}`)}
                </Chip>
              ))}
            </div>
          )}

          <SectionTitle>{t("shortcut.history")}</SectionTitle>
          <div className="px-4">
            <Sparkline points={q.data.history} label={t("shortcut.history")} />
          </div>

          <SectionTitle>{t("shortcut.report")}</SectionTitle>
          {!blocking ? (
            <div className="grid grid-cols-3 gap-2 px-4">
              <Button icon={<Check {...ICON} aria-hidden />} onClick={() => report.mutate({ verdict: "passable" })} disabled={report.isPending}>
                {t("shortcut.passable")}
              </Button>
              <Button icon={<Minus {...ICON} aria-hidden />} onClick={() => report.mutate({ verdict: "partly" })} disabled={report.isPending}>
                {t("shortcut.partly")}
              </Button>
              <Button icon={<X {...ICON} aria-hidden />} onClick={() => setBlocking(true)} disabled={report.isPending}>
                {t("shortcut.blocked")}
              </Button>
            </div>
          ) : (
            <div className="px-4">
              <p className="pb-2 text-sm text-ink-2">{t("shortcut.reason")}</p>
              <div className="flex flex-wrap gap-2">
                {REASONS.map((r) => (
                  <Chip key={r} onClick={() => report.mutate({ verdict: "blocked", reason: r })}>
                    {t(`shortcut.reason.${r}`)}
                  </Chip>
                ))}
              </div>
              <Button variant="ghost" className="mt-2" onClick={() => setBlocking(false)}>
                {t("common.cancel")}
              </Button>
            </div>
          )}

          <SectionTitle>{t("shortcut.reports")}</SectionTitle>
          {q.data.reports.length === 0 ? (
            <p className="px-4 text-sm text-ink-2">{t("shortcut.no_reports")}</p>
          ) : (
            <ul className="px-4 text-sm text-ink-2">
              {q.data.reports.slice(0, 8).map((r) => (
                <li key={`${r.day}-${r.verdict}-${r.reason}`} className="flex justify-between py-1">
                  <span>
                    {t(`shortcut.${r.verdict as "passable" | "blocked" | "partly"}`)}
                    {r.reason ? ` · ${t(`shortcut.reason.${r.reason as (typeof REASONS)[number]}`)}` : ""}
                    {r.count > 1 ? ` ×${r.count}` : ""}
                  </span>
                  <span>{formatAgo(`${r.day}T12:00:00Z`, lang)}</span>
                </li>
              ))}
            </ul>
          )}

          <div className="flex flex-col gap-2 px-4 pt-4">
            <Button
              variant="primary"
              icon={<Navigation {...ICON} aria-hidden />}
              onClick={() => {
                const pts = decodePolyline(sc.polyline);
                const end = pts[pts.length - 1]!;
                navigate(routeHref({ ...end, name: sc.title ?? t("shortcut.title_fallback") }));
              }}
            >
              {t("route.ride")}
            </Button>
            <a className="inline-flex h-12 items-center justify-center gap-2 text-sm text-ink-2 underline" href={q.data.osm_edit_url} target="_blank" rel="noreferrer noopener">
              <ExternalLink size={16} strokeWidth={1.75} aria-hidden /> {t("shortcut.suggest_osm")}
            </a>
          </div>
        </div>
      ) : null}
    </Panel>
  );
}
