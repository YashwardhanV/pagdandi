import { useEffect, useMemo, useRef, useState } from "react";
import maplibregl, { type GeoJSONSource } from "maplibre-gl";
import { api, type LatLng } from "../lib/api";
import { clock, duration, km } from "../lib/format";
import { pointAlong, sliceLine } from "../lib/geo";
import { trailColor } from "../lib/kinds";
import { locate, prepare, prompt, stateAt, type NavState } from "../lib/nav";
import { load, save } from "../lib/storage";
import { getMap } from "../map/MapView";
import { useStore } from "../state/store";
import { Icon, turnIcon } from "../ui/Icon";

/** Turn-by-turn ride view, with GPS or a simulated preview. */
export function Ride() {
  const ride = useStore((s) => s.ride);
  const endRide = useStore((s) => s.endRide);
  const vehicle = useStore((s) => s.vehicle);
  const showToast = useStore((s) => s.showToast);
  const [trail, setTrail] = useState(ride.trail!);
  const prep = useMemo(() => prepare(trail), [trail]);
  const [nav, setNav] = useState<NavState>(() => stateAt(prep, 0));
  const [voice, setVoice] = useState(() => load("voice", true));
  const [follow, setFollow] = useState(true);
  const followRef = useRef(true);
  const bearingRef = useRef(pointAlong(trail.geometry, prep.cum, 0).heading);
  const [gpsErr, setGpsErr] = useState<string | null>(null);
  const along = useRef(0);
  const spoken = useRef<Set<string>>(new Set());
  const offCount = useRef(0);
  const rerouting = useRef(false);
  const puck = useRef<maplibregl.Marker | null>(null);
  const color = trailColor(trail);

  // Puck, camera, progress line.
  const show = (p: LatLng, heading: number, st: NavState) => {
    const map = getMap();
    if (!map) return;
    if (!puck.current) {
      const el = document.createElement("div");
      el.className = "puck";
      el.innerHTML = '<span class="puck-arrow"></span>';
      puck.current = new maplibregl.Marker({ element: el, rotationAlignment: "map", pitchAlignment: "map" }).setLngLat([p.lng, p.lat]).addTo(map);
    }
    puck.current.setLngLat([p.lng, p.lat]).setRotation(heading);
    (map.getSource("progress") as GeoJSONSource | undefined)?.setData({
      type: "FeatureCollection",
      features: st.along > 5 ? [{ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: sliceLine(trail.geometry, prep.cum, 0, st.along) } }] : [],
    });
    if (followRef.current && ride.simulate) {
      // Per-frame updates: jump (an ease would restart every frame), smoothing the bearing.
      const cur = bearingRef.current;
      const diff = ((heading - cur + 540) % 360) - 180;
      bearingRef.current = (cur + diff * 0.08 + 360) % 360;
      map.jumpTo({ center: [p.lng, p.lat], bearing: bearingRef.current, pitch: 58, zoom: 15.6, padding: { top: 260, bottom: 140, left: 0, right: 0 } });
    } else if (followRef.current) map.easeTo({ center: [p.lng, p.lat], bearing: heading, pitch: 58, zoom: 16.2, duration: 900, padding: { top: 260, bottom: 140, left: 0, right: 0 }, essential: true });
  };

  const speak = (st: NavState) => {
    if (!voice || !("speechSynthesis" in window) || !st.step) return;
    const key = (band: string) => `${st.step!.index}:${band}`;
    const band = st.toStepM < 60 ? "now" : st.toStepM < 350 ? "soon" : null;
    if (!band || spoken.current.has(key(band))) return;
    spoken.current.add(key(band));
    const u = new SpeechSynthesisUtterance(prompt(st.step, st.toStepM));
    u.lang = "en-IN";
    u.rate = 1.02;
    window.speechSynthesis.cancel();
    window.speechSynthesis.speak(u);
  };

  // Simulated preview: glide along the trail in about a minute.
  useEffect(() => {
    if (!ride.simulate) return;
    (window as unknown as { __pdRiding?: boolean }).__pdRiding = true;
    const speed = Math.min(450, Math.max(25, prep.total / 60)); // m/s
    let last = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const dt = (now - last) / 1000;
      last = now;
      along.current = Math.min(prep.total, along.current + speed * dt);
      const st = stateAt(prep, along.current);
      const pa = pointAlong(trail.geometry, prep.cum, along.current);
      setNav(st);
      show(pa.p, pa.heading, st);
      if (along.current < prep.total) raf = requestAnimationFrame(tick);
    };
    const start = setTimeout(() => {
      last = performance.now();
      raf = requestAnimationFrame(tick);
    }, 600);
    return () => {
      clearTimeout(start);
      cancelAnimationFrame(raf);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ride.simulate, prep]);

  // Real GPS.
  useEffect(() => {
    if (ride.simulate) return;
    (window as unknown as { __pdRiding?: boolean }).__pdRiding = true;
    if (!("geolocation" in navigator)) {
      setGpsErr("This device can't share its location");
      return;
    }
    const id = navigator.geolocation.watchPosition(
      async (pos) => {
        setGpsErr(null);
        const p = { lat: pos.coords.latitude, lng: pos.coords.longitude };
        const st = locate(prep, p, along.current, pos.coords.accuracy);
        along.current = st.along;
        setNav(st);
        const pa = pointAlong(trail.geometry, prep.cum, st.along);
        const heading = pos.coords.heading != null && !Number.isNaN(pos.coords.heading) && (pos.coords.speed ?? 0) > 2 ? pos.coords.heading : pa.heading;
        show(st.offRoute ? p : pa.p, heading, st);
        speak(st);
        offCount.current = st.offRoute ? offCount.current + 1 : 0;
        if (offCount.current >= 3 && !rerouting.current) {
          rerouting.current = true;
          showToast("Off the trail. Finding a way back…");
          try {
            const end = trail.geometry[trail.geometry.length - 1];
            const r = await api.go({ from: p, to: { lat: end[1], lng: end[0] }, vehicle });
            const next = r.trails.find((t) => t.kind === trail.kind) ?? r.trails[0];
            if (next) {
              along.current = 0;
              spoken.current.clear();
              setTrail(next);
            }
          } catch {
            showToast("Couldn't reroute. Keep heading for the trail.", "warn");
          } finally {
            offCount.current = 0;
            rerouting.current = false;
          }
        }
      },
      (err) => setGpsErr(err.code === err.PERMISSION_DENIED ? "Location is blocked. Allow it to ride with turn-by-turn." : "Waiting for GPS…"),
      { enableHighAccuracy: true, maximumAge: 1000, timeout: 20000 },
    );
    return () => navigator.geolocation.clearWatch(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ride.simulate, prep]);

  // Keep the screen awake; restore the map when leaving.
  useEffect(() => {
    let lock: { release: () => Promise<void> } | null = null;
    const nav = navigator as unknown as { wakeLock?: { request: (t: string) => Promise<{ release: () => Promise<void> }> } };
    nav.wakeLock?.request("screen").then((l) => (lock = l)).catch(() => undefined);
    const map = getMap();
    const pa = pointAlong(trail.geometry, prep.cum, 0);
    map?.easeTo({ center: [pa.p.lng, pa.p.lat], bearing: pa.heading, pitch: 58, zoom: 16, duration: 1200, padding: { top: 260, bottom: 140, left: 0, right: 0 } });
    return () => {
      void lock?.release();
      (window as unknown as { __pdRiding?: boolean }).__pdRiding = false;
      puck.current?.remove();
      window.speechSynthesis?.cancel();
      const m = getMap();
      (m?.getSource("progress") as GeoJSONSource | undefined)?.setData({ type: "FeatureCollection", features: [] });
      m?.easeTo({ pitch: 0, bearing: 0, duration: 700, padding: { top: 0, bottom: 0, left: 0, right: 0 } });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const eta = new Date(Date.now() + nav.remainingS * 1000);
  const step = nav.step;

  if (nav.arrived && nav.along > 0) {
    return (
      <div className="ride-done" role="dialog" aria-label="Arrived">
        <div className="done-card" style={{ "--c": color } as React.CSSProperties}>
          <p className="eyebrow">You made it</p>
          <h2 className="done-title">{trail.name}</h2>
          <div className="done-stats">
            <span>
              <b>{km(trail.distance_m)}</b> ridden
            </span>
            <span>
              <b>{Math.round(trail.climb_m)} m</b> climbed
            </span>
            {trail.fresh_m > 0 && (
              <span>
                <b>{km(trail.fresh_m)}</b> of new road
              </span>
            )}
          </div>
          <button className="btn primary" onClick={endRide}>
            Done
          </button>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="hud" style={{ "--c": color } as React.CSSProperties} aria-live="polite">
        <div className="hud-turn">
          <span className="hud-ico">
            <Icon name={turnIcon(step?.sign ?? 0)} size={44} />
          </span>
          <span className="hud-dist">{km(nav.toStepM)}</span>
        </div>
        <div className="hud-text">
          <span className="hud-instr">{step?.text ?? "Follow the trail"}</span>
          {nav.ahead && <span className="hud-ahead">{nav.ahead}</span>}
          {ride.simulate && <span className="hud-sim">Preview</span>}
          {gpsErr && <span className="hud-err">{gpsErr}</span>}
        </div>
      </div>

      <div className="ride-bar" style={{ "--c": color } as React.CSSProperties}>
        <button className="icon-btn danger" onClick={endRide} aria-label="End ride">
          <Icon name="close" size={22} />
        </button>
        <div className="ride-stats">
          <span className="ride-eta">{clock(eta)}</span>
          <span className="ride-rest">
            {duration(nav.remainingS)} · {km(nav.remainingM)}
          </span>
          <span className="ride-prog" aria-hidden="true">
            <i style={{ width: `${(nav.along / (prep.total || 1)) * 100}%` }} />
          </span>
        </div>
        <button
          className="icon-btn"
          aria-label={voice ? "Mute voice" : "Unmute voice"}
          onClick={() => {
            save("voice", !voice);
            setVoice(!voice);
          }}
        >
          <Icon name={voice ? "volume" : "mute"} size={20} />
        </button>
        <button className={`icon-btn ${follow ? "on" : ""}`} aria-label="Follow position" onClick={() => {
            followRef.current = !follow;
            setFollow(!follow);
          }}>
          <Icon name="locate" size={20} />
        </button>
      </div>
    </>
  );
}
