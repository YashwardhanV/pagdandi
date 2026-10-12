import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import { ArrowLeft, Check, CloudDownload, Trash2 } from "lucide-react";
import { formatBytes } from "../../lib/format";
import { useT } from "../../lib/i18n";
import { useRegions, useTilesManifest } from "../../lib/queries";
import { recentRoutes } from "../../state/store";
import { Button, EmptyState, ICON, IconButton, ListRow, Skeleton } from "../../components/ui";
import { Panel } from "../../components/ui/Sheet";

const CACHE = "pd-offline-packs";

/**
 * Offline packs (SPEC §10.3): a region's PMTiles subset (z0–15 within 25 km)
 * stored in the Cache API, plus the last 20 routes kept in local storage.
 * Routing offline is not supported in v1.
 */
export default function OfflinePacks() {
  const t = useT();
  const navigate = useNavigate();
  const manifest = useTilesManifest();
  const regions = useRegions();
  const qc = useQueryClient();
  const cached = useQuery({
    queryKey: ["offline-cached"],
    queryFn: async () => {
      if (!("caches" in window)) return new Set<string>();
      const keys = await (await caches.open(CACHE)).keys();
      return new Set(keys.map((k) => new URL(k.url).pathname));
    },
  });
  const have = cached.data ?? new Set<string>();
  const [progress, setProgress] = useState<Record<string, number>>({});
  const refresh = () => qc.invalidateQueries({ queryKey: ["offline-cached"] });

  const download = async (id: string, file: string, bytes: number) => {
    const url = `/tiles/${file}`;
    const res = await fetch(url);
    if (!res.ok || !res.body) return;
    const reader = res.body.getReader();
    const chunks: Uint8Array[] = [];
    let got = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
      got += value.length;
      setProgress((p) => ({ ...p, [id]: Math.round((got / bytes) * 100) }));
    }
    const c = await caches.open(CACHE);
    await c.put(url, new Response(new Blob(chunks as BlobPart[]), { headers: { "Content-Type": "application/octet-stream", "Accept-Ranges": "bytes" } }));
    setProgress((p) => ({ ...p, [id]: 100 }));
    await refresh();
  };
  const remove = async (file: string) => {
    const c = await caches.open(CACHE);
    await c.delete(`/tiles/${file}`);
    await refresh();
  };

  const packs = Object.entries(manifest.data?.packs ?? {});
  const names = Object.fromEntries((regions.data?.regions ?? []).map((r) => [r.id, r.name]));

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate(-1)}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{t("offline.title")}</h1>
    </div>
  );

  return (
    <Panel title={t("offline.title")} snap={0.92} header={header}>
      <p className="px-4 pb-2 text-sm text-ink-2">{t("offline.hint")}</p>
      {manifest.isPending ? (
        <Skeleton className="mx-4 h-14" />
      ) : packs.length === 0 ? (
        <EmptyState title={t("offline.none")} />
      ) : (
        packs.map(([id, p]) => {
          const cached = have.has(`/tiles/${p.file}`);
          const pct = progress[id];
          return (
            <ListRow
              key={id}
              icon={cached ? <Check {...ICON} /> : <CloudDownload {...ICON} />}
              title={names[id] ?? id}
              subtitle={cached ? t("offline.downloaded") : pct != null && pct < 100 ? t("offline.downloading", { pct }) : formatBytes(p.bytes)}
              trailing={
                cached ? (
                  <Button variant="ghost" icon={<Trash2 {...ICON} aria-hidden />} onClick={() => remove(p.file)}>
                    {t("offline.remove")}
                  </Button>
                ) : (
                  <Button onClick={() => download(id, p.file, p.bytes)} disabled={pct != null && pct < 100}>
                    {t("offline.download", { size: formatBytes(p.bytes) })}
                  </Button>
                )
              }
            />
          );
        })
      )}
      <p className="px-4 pt-4 text-sm text-ink-2">{t("offline.recent_routes", { n: recentRoutes().length })}</p>
      <p className="px-4 pt-2 pb-6 text-sm text-ink-2">{t("route.offline")}</p>
    </Panel>
  );
}
