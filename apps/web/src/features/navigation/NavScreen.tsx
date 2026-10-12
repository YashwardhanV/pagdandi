import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import {
  ArrowUp, ArrowUpLeft, ArrowUpRight, CornerUpLeft, CornerUpRight, Flag, RotateCw, Undo2, Volume2, VolumeX, X, TriangleAlert,
} from "lucide-react";
import { ApiError, api, type Instruction, type RouteResponse } from "../../lib/api";
import { formatClock, formatDistance } from "../../lib/format";
import { watchPosition } from "../../lib/geolocation";
import { unpavedKey, useT } from "../../lib/i18n";
import { useRegion } from "../../lib/queries";
import { recentRoutes, useApp } from "../../state/store";
import { Button, EmptyState, ICON, IconButton, toast } from "../../components/ui";
import { useMapStore } from "../map/mapStore";
import { holdWakeLock, speak, stopSpeaking } from "./device";
import { arrived as hasArrived, communityShortcut, nextManeuver, OffRouteTracker, prepare, progress, surfaceAhead, type NavRoute } from "./engine";

const BIG = { size: 40, strokeWidth: 2 } as const;

export function ManeuverIcon({ sign, ...p }: { sign: number; size?: number; strokeWidth?: number }) {
  switch (sign) {
    case -3:
    case -2:
      return <CornerUpLeft {...p} aria-hidden />;
    case 3:
    case 2:
      return <CornerUpRight {...p} aria-hidden />;
    case -1:
    case -7:
      return <ArrowUpLeft {...p} aria-hidden />;
    case 1:
    case 7:
      return <ArrowUpRight {...p} aria-hidden />;
    case 4:
    case 5:
      return <Flag {...p} aria-hidden />;
    case 6:
      return <RotateCw {...p} aria-hidden />;
    case -8:
    case 8:
    case -98:
      return <Undo2 {...p} aria-hidden />;
    default:
      return <ArrowUp {...p} aria-hidden />;
  }
}

function findRoute(id: string): RouteResponse | null {
  const cur = useApp.getState().route;
  if (cur?.request_id === id) return cur;
  return recentRoutes().find((x) => x.r.request_id === id)?.r ?? null;
}

export default function NavScreen() {
  const t = useT();
  const navigate = useNavigate();
  const { routeId = "", optionId = "" } = useParams();
  const { lang, muted, userPos, set } = useApp();
  const region = useRegion();
  const setMap = useMapStore((s) => s.set);

  const initial = useMemo(() => findRoute(routeId), [routeId]);
  const [nav, setNav] = useState<NavRoute | null>(() => {
    const o = initial?.options.find((x) => x.id === optionId);
    return o ? prepare(o) : null;
  });
  const [prog, setProg] = useState<ReturnType<typeof progress> | null>(null);
  const [rerouting, setRerouting] = useState(false);
  const [done, setDone] = useState(false);
  const tracker = useRef(new OffRouteTracker());
  const spoken = useRef(new Set<string>());
  const navRef = useRef(nav);
  useEffect(() => {
    navRef.current = nav;
  }, [nav]);
  const [usedShortcut] = useState<string | null>(() => (nav ? communityShortcut(nav.option) : null));
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (initial) set({ route: initial, selected: optionId });
  }, [initial, optionId, set]);

  const say = (key: string, text: string) => {
    if (spoken.current.has(key)) return;
    spoken.current.add(key);
    if (!useApp.getState().muted) speak(text, useApp.getState().lang);
  };

  const reroute = async (p: { lat: number; lng: number }) => {
    const cur = navRef.current;
    if (!cur || rerouting) return;
    setRerouting(true);
    say(`reroute-${Date.now()}`, t("nav.rerouting"));
    try {
      const end = cur.line[cur.line.length - 1]!;
      const res = await api.route({
        from: { lat: p.lat, lng: p.lng },
        to: end,
        vehicle: initial?.context.vehicle,
        mode: cur.option.kind === "shortcut" ? "shortcut" : "standard",
        lang,
      });
      const o = res.options.find((x) => x.kind === cur.option.kind) ?? res.options.find((x) => x.id === res.recommended)!;
      set({ route: res, selected: o.id });
      setNav(prepare(o));
      spoken.current.clear();
      tracker.current.reset();
    } catch (e) {
      toast((e as ApiError).message);
    } finally {
      setRerouting(false);
    }
  };

  const onFix = useEffectEvent((p: { lat: number; lng: number; accuracy: number }) => {
    setNow(Date.now());
    const r = navRef.current;
    if (!r) return;
    const pr = progress(r, p);
    setProg(pr);
    if (hasArrived(r, p, pr.alongM)) {
      setDone(true);
      say("arrived", t("nav.arrived"));
      return;
    }
    if (tracker.current.update(pr.offM, p.accuracy)) {
      void reroute(p);
      return;
    }
    const m = nextManeuver(r, pr.alongM);
    if (m) {
      const k = `${m.instruction.interval[0]}`;
      if (m.distanceM < 220 && m.distanceM > 60) say(`far-${k}`, `${formatDistance(m.distanceM, lang)}, ${m.instruction.text}`);
      if (m.distanceM <= 40) say(`near-${k}`, m.instruction.text);
    }
    const ahead = surfaceAhead(r, pr.alongM, 300);
    if (ahead && ahead.inM <= 220) {
      const surface = ahead.kind === "track" ? t("surface.track") : t(unpavedKey(region?.country));
      say(`surf-${ahead.startIdx}`, t("nav.surface_ahead", { surface, dist: formatDistance(Math.max(ahead.inM, 50), lang), len: formatDistance(ahead.lengthM, lang) }));
    }
  });

  // Wake lock, follow mode and GPS while navigating.
  useEffect(() => {
    if (!nav) return;
    const release = holdWakeLock();
    setMap({ follow: true });
    const stop = watchPosition((p) => onFix(p), () => toast(t("nav.gps_needed")));
    return () => {
      release();
      stop();
      stopSpeaking();
      setMap({ follow: false });
    };
  }, [nav === null]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!nav) {
    return (
      <div className="fixed inset-x-0 bottom-0 z-20 rounded-t-lg bg-surface shadow-sheet lg:inset-y-0 lg:left-0 lg:w-[400px] lg:rounded-none">
        <EmptyState title={t("route.out_of_coverage")} body="" action={<Button onClick={() => navigate("/")}>{t("common.back")}</Button>} />
      </div>
    );
  }

  const along = prog?.alongM ?? 0;
  const man = nextManeuver(nav, along);
  const ahead = surfaceAhead(nav, along, 300);
  const remainingM = prog?.remainingM ?? nav.total;
  const remainingS = nav.total > 0 ? (nav.option.duration_s * remainingM) / nav.total : 0;
  const eta = new Date(now + remainingS * 1000);

  if (done) {
    return <ArrivalSheet shortcutId={usedShortcut} onDone={() => navigate("/")} />;
  }

  return (
    <>
      <div className="fixed inset-x-0 top-0 z-20 p-3 pt-[max(32px,calc(env(safe-area-inset-top)+24px))] lg:left-[400px] lg:max-w-xl" aria-live="polite">
        <div className="flex items-center gap-4 rounded-lg bg-ink p-4 text-bg shadow-float" data-testid="maneuver">
          {man && <ManeuverIcon sign={man.instruction.sign} {...BIG} />}
          <div className="min-w-0 flex-1">
            {!userPos ? (
              <p className="text-lg">{t("nav.gps_waiting")}</p>
            ) : rerouting ? (
              <p className="text-lg">{t("nav.rerouting")}</p>
            ) : man ? (
              <>
                <p className="num text-2xl leading-tight">{formatDistance(man.distanceM, lang)}</p>
                <p className="truncate text-base">{man.instruction.text}</p>
              </>
            ) : null}
          </div>
        </div>
        {man?.next && man.distanceM < 300 && <NextHint ins={man.next} />}
        {ahead && (
          <div role="status" className="mt-2 flex items-center gap-2 rounded-md bg-surface px-3 py-2 text-sm text-warn shadow-float" data-testid="surface-ahead">
            <TriangleAlert size={18} strokeWidth={1.75} aria-hidden />
            <span className="text-ink">
              {t("nav.surface_ahead", {
                surface: ahead.kind === "track" ? t("surface.track") : t(unpavedKey(region?.country)),
                dist: formatDistance(Math.max(ahead.inM, 10), lang),
                len: formatDistance(ahead.lengthM, lang),
              })}
            </span>
          </div>
        )}
      </div>

      <div className="fixed inset-x-0 bottom-0 z-20 flex items-center gap-3 rounded-t-lg bg-surface px-4 pt-3 pb-[max(16px,env(safe-area-inset-bottom))] shadow-sheet lg:inset-x-auto lg:bottom-4 lg:left-[416px] lg:w-[400px] lg:rounded-lg">
        <div className="min-w-0 flex-1">
          <p className="num text-xl text-ink">{t("nav.eta", { time: formatClock(eta, lang) })}</p>
          <p className="num text-sm text-ink-2">{t("nav.remaining", { dist: formatDistance(remainingM, lang) })}</p>
        </div>
        <IconButton label={muted ? t("nav.unmute") : t("nav.mute")} onClick={() => set({ muted: !muted })} aria-pressed={muted}>
          {muted ? <VolumeX {...ICON} /> : <Volume2 {...ICON} />}
        </IconButton>
        <Button variant="danger" icon={<X {...ICON} aria-hidden />} onClick={() => navigate(-1)}>
          {t("nav.end")}
        </Button>
      </div>
    </>
  );
}

function NextHint({ ins }: { ins: Instruction }) {
  const t = useT();
  return (
    <div className="mt-2 inline-flex items-center gap-2 rounded-md bg-ink px-3 py-1.5 text-sm text-bg opacity-90">
      <span>{t("nav.then")}</span>
      <ManeuverIcon sign={ins.sign} size={18} strokeWidth={2} />
    </div>
  );
}

/** One-question post-ride prompt when a community shortcut was used (SPEC §7.2, §10.2.4). */
function ArrivalSheet({ shortcutId, onDone }: { shortcutId: string | null; onDone: () => void }) {
  const t = useT();
  const [answered, setAnswered] = useState(false);
  const answer = async (yes: boolean) => {
    if (!shortcutId) return;
    try {
      await api.report(shortcutId, yes ? { verdict: "passable" } : { verdict: "blocked", reason: "other" });
      setAnswered(true);
    } catch (e) {
      const err = e as ApiError;
      if (err.status === 401) toast(t("common.sign_in_needed"));
      else if (err.code === "ALREADY_REPORTED") setAnswered(true);
      else toast(err.message);
    }
  };
  return (
    <div className="fixed inset-x-0 bottom-0 z-20 flex flex-col gap-3 rounded-t-lg bg-surface p-4 pb-[max(16px,env(safe-area-inset-bottom))] shadow-sheet lg:inset-y-0 lg:left-0 lg:w-[400px] lg:rounded-none" data-testid="arrival">
      <p className="flex items-center gap-2 text-lg font-semibold text-ink">
        <Flag {...ICON} aria-hidden /> {t("nav.arrived")}
      </p>
      {shortcutId && !answered && (
        <>
          <p className="text-base text-ink">{t("nav.post_ride")}</p>
          <div className="flex gap-2">
            <Button block onClick={() => answer(false)}>
              {t("common.no")}
            </Button>
            <Button block variant="primary" onClick={() => answer(true)}>
              {t("common.yes")}
            </Button>
          </div>
          <Link to="/signin" className="text-center text-xs text-ink-2 underline">
            {t("common.sign_in")}
          </Link>
        </>
      )}
      {answered && <p className="text-sm text-ok">{t("nav.thanks")}</p>}
      <Button onClick={onDone}>{t("common.done")}</Button>
    </div>
  );
}
