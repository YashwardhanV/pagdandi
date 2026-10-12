import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as Switch from "@radix-ui/react-switch";
import { ArrowLeft, Circle, PenLine, Square, Undo2 } from "lucide-react";
import { ApiError, api, type Flag, type Surface, type Vehicle } from "../../lib/api";
import { haversine, lineLength, type LatLng } from "../../lib/geo";
import { formatDistance } from "../../lib/format";
import { watchPosition } from "../../lib/geolocation";
import { unpavedKey, useT } from "../../lib/i18n";
import { useMe, useRegion } from "../../lib/queries";
import { useApp } from "../../state/store";
import { Button, Chip, EmptyState, ICON, IconButton, SegmentedControl, toast } from "../../components/ui";
import { Panel, SectionTitle } from "../../components/ui/Sheet";
import { confirmLocation } from "../../components/LocateExplainer";
import { useMapStore } from "../map/mapStore";

type Fix = LatLng & { t: string };
const FLAGS: Flag[] = ["gate", "floods_in_rain", "steep", "narrow", "no_lights", "livestock"];
const key = (p: LatLng) => `${p.lat.toFixed(6)},${p.lng.toFixed(6)}`;

/** Drop the first `startM` and last `endM` metres of a recorded ride. */
export function trimFixes(fixes: Fix[], startM: number, endM: number): Fix[] {
  if (fixes.length < 2) return fixes;
  const cum = [0];
  for (let i = 1; i < fixes.length; i++) cum.push(cum[i - 1]! + haversine(fixes[i - 1]!, fixes[i]!));
  const total = cum[cum.length - 1]!;
  return fixes.filter((_, i) => cum[i]! >= startM && cum[i]! <= total - endM);
}

export default function AddShortcut() {
  const t = useT();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const region = useRegion();
  const lang = useApp((s) => s.lang);
  const setMap = useMapStore((s) => s.set);

  const [mode, setMode] = useState<"draw" | "record">("draw");
  const [points, setPoints] = useState<LatLng[]>([]);
  const [snaps, setSnaps] = useState<Record<string, { ok: boolean; distance_m: number; lat: number; lng: number }>>({});
  const [fixes, setFixes] = useState<Fix[]>([]);
  const [recording, setRecording] = useState(false);
  const [trim, setTrim] = useState({ start: 0, end: 0 });
  const [step, setStep] = useState<"trace" | "details">("trace");
  const [surface, setSurface] = useState<Surface>("unpaved");
  const [pickedVehicles, setVehicles] = useState<Vehicle[] | null>(null);
  const vehicles = pickedVehicles ?? region?.vehicles ?? [];
  const [flags, setFlags] = useState<Flag[]>([]);
  const [title, setTitle] = useState("");
  const [oneway, setOneway] = useState(false);
  const stopRef = useRef<(() => void) | null>(null);

  const snapCheck = useCallback((p: LatLng) => {
    api.snap(p.lat, p.lng).then((r) => setSnaps((s) => ({ ...s, [key(p)]: r }))).catch(() => {});
  }, []);

  const trimmed = useMemo(() => trimFixes(fixes, trim.start, trim.end), [fixes, trim]);
  const trace: LatLng[] = mode === "draw" ? points : trimmed;

  // Show the draft on the map and capture taps in draw mode.
  useEffect(() => {
    const snapState = trace.map((p, i) => {
      if (i !== 0 && i !== trace.length - 1) return null;
      const s = snaps[key(p)];
      return s ? { ok: s.ok } : null;
    });
    setMap({ draft: { points: trace, snaps: snapState } });
  }, [trace, snaps, setMap]);

  useEffect(() => {
    if (mode !== "draw" || step !== "trace") {
      setMap({ onClick: null });
      return;
    }
    setMap({
      onClick: (p) => {
        setPoints((prev) => [...prev, p]);
        snapCheck(p);
      },
    });
    return () => setMap({ onClick: null });
  }, [mode, step, setMap, snapCheck]);

  useEffect(() => () => {
    stopRef.current?.();
    setMap({ draft: null, onClick: null });
  }, [setMap]);

  const startRecording = async () => {
    if (!(await confirmLocation())) return;
    setFixes([]);
    setRecording(true);
    stopRef.current = watchPosition((p) => {
      if (p.accuracy > 40) return;
      setFixes((prev) => {
        const last = prev[prev.length - 1];
        if (last && haversine(last, p) < 5) return prev;
        return [...prev, { lat: p.lat, lng: p.lng, t: new Date(p.at).toISOString() }];
      });
    });
  };
  const stopRecording = () => {
    stopRef.current?.();
    stopRef.current = null;
    setRecording(false);
    const last = fixes[fixes.length - 1];
    if (fixes[0]) snapCheck(fixes[0]);
    if (last) snapCheck(last);
  };

  const ends = trace.length >= 2 ? [trace[0]!, trace[trace.length - 1]!] : [];
  const endSnaps = ends.map((p) => snaps[key(p)]);
  const ready = trace.length >= 2 && endSnaps.every((s) => s?.ok);

  const submit = useMutation({
    mutationFn: () =>
      api.createShortcut({
        source: mode === "draw" ? "drawn" : "recorded",
        points: mode === "draw" ? points : trimmed,
        surface,
        vehicles,
        flags,
        oneway,
        title: title.trim() || undefined,
      }),
    onSuccess: (r) => {
      toast(t("add.submitted"));
      void qc.invalidateQueries({ queryKey: ["shortcuts-view"] });
      navigate(`/s/${r.shortcut.id}`, { replace: true });
    },
    onError: (e: ApiError) => (e.status === 401 ? navigate("/signin?redirect=/add") : toast(e.message)),
  });

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => (step === "details" ? setStep("trace") : navigate(-1))}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{step === "details" ? t("add.details") : t("add.title")}</h1>
    </div>
  );

  if (me.data === null) {
    return (
      <Panel title={t("add.title")} header={header}>
        <EmptyState title={t("profile.signed_out")} action={<Link className="text-accent underline" to="/signin?redirect=/add">{t("common.sign_in")}</Link>} />
      </Panel>
    );
  }

  return (
    <Panel title={t("add.title")} snap={step === "details" ? 0.92 : 0.52} header={header}>
      {step === "trace" ? (
        <div className="flex flex-col gap-3 px-4 pb-6">
          <SegmentedControl
            label={t("add.title")}
            value={mode}
            onChange={(m) => {
              setMode(m);
              setPoints([]);
              setFixes([]);
            }}
            options={[
              { value: "draw", label: t("add.draw"), icon: <PenLine size={16} strokeWidth={1.75} aria-hidden /> },
              { value: "record", label: t("add.record"), icon: <Circle size={16} strokeWidth={1.75} aria-hidden /> },
            ]}
          />
          {mode === "draw" ? (
            <>
              <p className="text-sm text-ink-2">{t("add.draw_hint")}</p>
              <p className="num text-sm text-ink" data-testid="draw-count">
                {t("add.points", { n: points.length, len: formatDistance(lineLength(points), lang) })}
              </p>
            </>
          ) : (
            <>
              <p className="text-sm text-ink-2">{t("add.record_hint")}</p>
              {recording ? (
                <Button variant="danger" icon={<Square {...ICON} aria-hidden />} onClick={stopRecording}>
                  {t("add.stop_recording")} · {formatDistance(lineLength(fixes), lang)}
                </Button>
              ) : (
                <Button variant="primary" icon={<Circle {...ICON} aria-hidden />} onClick={startRecording}>
                  {t("add.start_recording")}
                </Button>
              )}
              {!recording && fixes.length > 1 && (
                <fieldset className="flex flex-col gap-2">
                  <legend className="pb-1 text-sm font-medium text-ink">{t("add.trim")}</legend>
                  {(["start", "end"] as const).map((k) => (
                    <label key={k} className="flex items-center gap-3 text-sm text-ink-2">
                      <span className="w-24">{t(k === "start" ? "add.trim_start" : "add.trim_end")}</span>
                      <input
                        type="range"
                        min={0}
                        max={Math.max(0, Math.floor(lineLength(fixes) / 2))}
                        step={5}
                        value={trim[k]}
                        onChange={(e) => setTrim((s) => ({ ...s, [k]: Number(e.target.value) }))}
                        className="h-12 flex-1 accent-[var(--accent)]"
                      />
                      <span className="num w-16 text-right">{formatDistance(trim[k], lang)}</span>
                    </label>
                  ))}
                </fieldset>
              )}
            </>
          )}
          {ends.map((_p, i) => {
            const s = endSnaps[i];
            if (!s) return null;
            return (
              <p key={i} className={s.ok ? "text-sm text-ok" : "text-sm text-danger"} role="status">
                {i === 0 ? t("search.from") : t("search.to")}: {s.ok ? t("add.snap_ok") : t("add.snap_far", { d: formatDistance(s.distance_m, lang) })}
              </p>
            );
          })}
          <div className="flex gap-2">
            {mode === "draw" && (
              <Button icon={<Undo2 {...ICON} aria-hidden />} disabled={!points.length} onClick={() => setPoints((p) => p.slice(0, -1))}>
                {t("common.undo")}
              </Button>
            )}
            <Button variant="primary" block disabled={!ready || recording} onClick={() => setStep("details")} data-testid="add-next">
              {trace.length < 2 ? t("add.need_two") : t("common.done")}
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="flex flex-col gap-1 pb-6"
          onSubmit={(e) => {
            e.preventDefault();
            submit.mutate();
          }}
        >
          <SectionTitle>{t("add.surface")}</SectionTitle>
          <div className="px-4">
            <SegmentedControl<Surface>
              label={t("add.surface")}
              value={surface}
              onChange={setSurface}
              options={[
                { value: "paved", label: t("surface.paved") },
                { value: "unpaved", label: t(unpavedKey(region?.country)) },
                { value: "track", label: t("surface.track") },
              ]}
            />
          </div>
          <SectionTitle>{t("add.vehicles")}</SectionTitle>
          <div className="flex flex-wrap gap-2 px-4">
            {(region?.vehicles ?? []).map((v) => (
              <Chip
                key={v}
                pressed={vehicles.includes(v)}
                onClick={() => setVehicles(vehicles.includes(v) ? (vehicles.length > 1 ? vehicles.filter((x) => x !== v) : vehicles) : [...vehicles, v])}
              >
                {t(`vehicle.${v}`)}
              </Chip>
            ))}
          </div>
          <SectionTitle>{t("add.flags")}</SectionTitle>
          <div className="flex flex-wrap gap-2 px-4">
            {FLAGS.map((f) => (
              <Chip key={f} pressed={flags.includes(f)} onClick={() => setFlags((cur) => (cur.includes(f) ? cur.filter((x) => x !== f) : [...cur, f]))}>
                {t(`flag.${f}`)}
              </Chip>
            ))}
          </div>
          <label className="flex flex-col gap-1 px-4 pt-4">
            <span className="text-sm font-medium text-ink">{t("add.title_label")}</span>
            <input
              value={title}
              maxLength={80}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("add.title_placeholder")}
              className="h-12 rounded-md border border-line bg-surface px-3 text-base text-ink placeholder:text-ink-3"
            />
          </label>
          <label className="flex min-h-14 items-center justify-between px-4">
            <span className="text-base text-ink">{t("add.oneway")}</span>
            <Switch.Root
              checked={oneway}
              onCheckedChange={setOneway}
              className="relative h-7 w-12 rounded-full bg-line data-[state=checked]:bg-accent"
            >
              <Switch.Thumb className="block h-6 w-6 translate-x-0.5 rounded-full bg-surface shadow-float transition-transform data-[state=checked]:translate-x-[22px]" />
            </Switch.Root>
          </label>
          <p className="px-4 pt-2 text-sm text-ink-2">{t("add.goes_live")}</p>
          <div className="px-4 pt-3">
            <Button type="submit" variant="primary" block disabled={submit.isPending}>
              {t("common.submit")}
            </Button>
          </div>
        </form>
      )}
    </Panel>
  );
}
