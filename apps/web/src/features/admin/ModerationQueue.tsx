import { useNavigate } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, Eye, Trash2, X } from "lucide-react";
import { ApiError, api } from "../../lib/api";
import { formatAgo, formatDistance } from "../../lib/format";
import { useT } from "../../lib/i18n";
import { useApp } from "../../state/store";
import { Button, Chip, EmptyState, ErrorState, ICON, IconButton, Skeleton, toast } from "../../components/ui";
import { Panel } from "../../components/ui/Sheet";

/** Moderation queue (SPEC §7.5): new shortcuts and private/unsafe reports. */
export default function ModerationQueue() {
  const t = useT();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const lang = useApp((s) => s.lang);
  const q = useQuery({ queryKey: ["admin-queue"], queryFn: api.adminQueue, retry: false });
  const act = useMutation({
    mutationFn: (v: { id: string; action: "approve" | "remove" | "dismiss" }) => api.adminAction(v.id, { action: v.action }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["admin-queue"] }),
    onError: (e: Error) => toast(e.message),
  });

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate(-1)}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{t("admin.title")}</h1>
    </div>
  );

  return (
    <Panel title={t("admin.title")} snap={0.92} header={header}>
      {q.isPending ? (
        <div className="flex flex-col gap-2 px-4">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      ) : q.isError ? (
        <ErrorState message={(q.error as ApiError).message} onRetry={() => q.refetch()} retryLabel={t("common.retry")} />
      ) : q.data.items.length === 0 ? (
        <EmptyState title={t("admin.empty")} />
      ) : (
        <ul className="divide-y divide-line">
          {q.data.items.map((it) => (
            <li key={`${it.kind}-${it.shortcut.id}-${it.created_at}`} className="flex flex-col gap-2 px-4 py-3">
              <div className="flex items-center gap-2">
                <Chip tone={it.kind === "report" ? "danger" : "neutral"}>
                  {it.kind === "report" ? t("admin.report", { reason: it.reason ? t(`shortcut.reason.${it.reason as "private"}`) : "" }) : t("admin.new")}
                </Chip>
                <span className="text-xs text-ink-2">{formatAgo(it.created_at, lang)}</span>
              </div>
              <p className="text-base text-ink">
                {it.shortcut.title ?? t("shortcut.title_fallback")} · <span className="num">{formatDistance(it.shortcut.length_m, lang)}</span>
              </p>
              {it.note && <p className="text-sm text-ink-2">“{it.note}”</p>}
              <div className="flex flex-wrap gap-2">
                <Button icon={<Eye {...ICON} aria-hidden />} onClick={() => navigate(`/s/${it.shortcut.id}`)}>
                  {t("admin.view")}
                </Button>
                {it.kind === "new_shortcut" && (
                  <Button variant="primary" icon={<Check {...ICON} aria-hidden />} onClick={() => act.mutate({ id: it.shortcut.id, action: "approve" })}>
                    {t("admin.approve")}
                  </Button>
                )}
                <Button variant="danger" icon={<Trash2 {...ICON} aria-hidden />} onClick={() => act.mutate({ id: it.shortcut.id, action: "remove" })}>
                  {t("admin.remove")}
                </Button>
                {it.kind === "report" && (
                  <Button variant="ghost" icon={<X {...ICON} aria-hidden />} onClick={() => act.mutate({ id: it.shortcut.id, action: "dismiss" })}>
                    {t("admin.dismiss")}
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
