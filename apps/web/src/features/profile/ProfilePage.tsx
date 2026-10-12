import { useNavigate } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as Switch from "@radix-ui/react-switch";
import { ArrowLeft, BadgeCheck, Briefcase, CloudDownload, Home, LogOut, Route as RouteIcon, ShieldCheck, Trash2 } from "lucide-react";
import { api, type Vehicle } from "../../lib/api";
import { formatDistance } from "../../lib/format";
import { LANGS, useT } from "../../lib/i18n";
import { useMe, useRegion } from "../../lib/queries";
import { useApp, type Theme } from "../../state/store";
import { Button, Chip, EmptyState, ErrorState, ICON, IconButton, ListRow, SegmentedControl, Skeleton, toast } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";

export default function ProfilePage() {
  const t = useT();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const region = useRegion();
  const { lang, theme, vehicle, home, work, set } = useApp();
  const user = me.data?.user;

  const contributions = useQuery({ queryKey: ["contributions"], enabled: !!user, queryFn: api.contributions });
  const patch = useMutation({
    mutationFn: api.patchMe,
    onSuccess: (r) => qc.setQueryData(["me"], r),
    onError: (e: Error) => toast(e.message),
  });
  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: () => qc.setQueryData(["me"], null),
  });
  const del = useMutation({
    mutationFn: api.deleteMe,
    onSuccess: () => {
      qc.setQueryData(["me"], null);
      toast(t("profile.deleted"));
      navigate("/");
    },
  });

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate("/")}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{t("profile.title")}</h1>
    </div>
  );

  const vehicles: Vehicle[] = region?.vehicles ?? ["scooter", "motorcycle"];

  return (
    <Panel title={t("profile.title")} snap={0.92} header={header}>
      {me.isPending ? (
        <div className="flex flex-col gap-2 px-4">
          <Skeleton className="h-8 w-40" />
          <Skeleton className="h-16" />
        </div>
      ) : me.isError ? (
        <ErrorState message={(me.error as Error).message} onRetry={() => me.refetch()} retryLabel={t("common.retry")} />
      ) : !user ? (
        <EmptyState title={t("profile.signed_out")} action={<Button variant="primary" onClick={() => navigate("/signin")}>{t("common.sign_in")}</Button>} />
      ) : (
        <>
          <div className="flex items-center gap-3 px-4 pb-2">
            <div className="min-w-0 flex-1">
              <p className="truncate text-lg font-semibold text-ink">{user.display_name}</p>
              <p className="truncate text-sm text-ink-2">{user.email}</p>
            </div>
            {me.data?.trusted && (
              <Chip tone="ok" icon={<BadgeCheck size={14} strokeWidth={1.75} aria-hidden />}>
                {t("profile.trusted")}
              </Chip>
            )}
          </div>

          <SectionTitle>{t("profile.contributions")}</SectionTitle>
          {contributions.isPending ? (
            <Skeleton className="mx-4 h-14" />
          ) : contributions.data && contributions.data.shortcuts.length === 0 ? (
            <EmptyState title={t("profile.no_contributions")} action={<Button onClick={() => navigate("/add")}>{t("home.add_shortcut")}</Button>} />
          ) : (
            <>
              <p className="px-4 text-sm text-ink-2">
                {t("profile.shortcuts_added", { n: contributions.data?.shortcuts.length ?? 0 })} · {t("profile.reports_made", { n: contributions.data?.reports ?? 0 })}
              </p>
              {contributions.data?.shortcuts.slice(0, 10).map((s) => (
                <ListRow
                  key={s.id}
                  icon={<RouteIcon {...ICON} />}
                  title={s.title ?? t("shortcut.title_fallback")}
                  subtitle={t(`shortcut.status.${s.status}`)}
                  trailing={<span className="num">{formatDistance(s.length_m, lang)}</span>}
                  onClick={() => navigate(`/s/${s.id}`)}
                />
              ))}
            </>
          )}
        </>
      )}

      <SectionTitle>{t("profile.settings")}</SectionTitle>
      <div className="flex flex-col gap-4 px-4">
        <div>
          <p className="pb-1 text-sm font-medium text-ink">{t("profile.language")}</p>
          <SegmentedControl label={t("profile.language")} value={lang} onChange={(v) => set({ lang: v })} options={LANGS.map((l) => ({ value: l.id, label: l.label }))} />
        </div>
        <div>
          <p className="pb-1 text-sm font-medium text-ink">{t("profile.theme")}</p>
          <SegmentedControl<Theme>
            label={t("profile.theme")}
            value={theme}
            onChange={(v) => set({ theme: v })}
            options={(["auto", "light", "dark"] as Theme[]).map((v) => ({ value: v, label: t(`profile.theme.${v}`) }))}
          />
        </div>
        <div>
          <p className="pb-1 text-sm font-medium text-ink">{t("profile.vehicle")}</p>
          <SegmentedControl<Vehicle>
            label={t("profile.vehicle")}
            value={vehicle && vehicles.includes(vehicle) ? vehicle : vehicles[0]!}
            onChange={(v) => set({ vehicle: v })}
            options={vehicles.map((v) => ({ value: v, label: t(`vehicle.${v}`) }))}
          />
        </div>
        {user && (
          <label className="flex items-start justify-between gap-4">
            <span>
              <span className="block text-base text-ink">{t("profile.traces")}</span>
              <span className="block text-sm text-ink-2">{t("profile.traces_hint")}</span>
            </span>
            <Switch.Root
              checked={!!user.traces_consent_at}
              onCheckedChange={(v) => patch.mutate({ traces_consent: v })}
              className="relative mt-1 h-7 w-12 shrink-0 rounded-full bg-line data-[state=checked]:bg-accent"
            >
              <Switch.Thumb className="block h-6 w-6 translate-x-0.5 rounded-full bg-surface shadow-float transition-transform data-[state=checked]:translate-x-[22px]" />
            </Switch.Root>
          </label>
        )}
      </div>
      <div className="pt-2">
        <ListRow icon={<Home {...ICON} />} title={t("search.home")} subtitle={home?.name ?? t("search.set_home")} onClick={() => navigate("/search?field=home")} />
        <ListRow icon={<Briefcase {...ICON} />} title={t("search.work")} subtitle={work?.name ?? t("search.set_work")} onClick={() => navigate("/search?field=work")} />
        <ListRow icon={<CloudDownload {...ICON} />} title={t("profile.offline")} onClick={() => navigate("/offline")} />
        {user && (user.role === "moderator" || user.role === "admin") && (
          <ListRow icon={<ShieldCheck {...ICON} />} title={t("profile.moderation")} onClick={() => navigate("/admin")} />
        )}
      </div>
      {user && (
        <div className="flex flex-col gap-2 px-4 pt-4 pb-8">
          <Button icon={<LogOut {...ICON} aria-hidden />} onClick={() => logout.mutate()}>
            {t("common.sign_out")}
          </Button>
          <Button
            variant="danger"
            icon={<Trash2 {...ICON} aria-hidden />}
            onClick={() => {
              if (window.confirm(t("profile.delete_confirm"))) del.mutate();
            }}
          >
            {t("profile.delete")}
          </Button>
        </div>
      )}
    </Panel>
  );
}
