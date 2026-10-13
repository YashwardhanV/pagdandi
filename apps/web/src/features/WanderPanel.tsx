import { useEffect, useState } from "react";
import type { Place } from "../lib/api";
import { FLAVOURS, placeKind } from "../lib/kinds";
import { origin, selectedTrail, useStore } from "../state/store";
import { Range, Segmented, Spinner, VehicleDial } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { TrailCard, TrailSkeleton } from "./TrailCard";
import { TrailDetail } from "./TrailDetail";

// Slider position 0..100 ↔ km on an exponential scale (fine control near home).
const toKm = (x: number, max: number) => {
  const v = Math.pow(max, x / 100);
  return v < 10 ? Math.max(1, Math.round(v)) : v < 30 ? Math.round(v) : Math.round(v / 5) * 5;
};
const toX = (km: number, max: number) => (Math.log(Math.max(1, km)) / Math.log(max)) * 100;

const COMPASS = ["N", "NE", "E", "SE", "S", "SW", "W", "NW"];
const dir = (b: number) => COMPASS[Math.round(b / 45) % 8];

export function WanderPanel() {
  const s = useStore();
  const w = s.wander;
  const v = s.meta?.vehicles.find((x) => x.id === s.vehicle);
  const max = v?.max_km ?? 100;
  const [x, setX] = useState(() => toX(w.radiusKm, max));
  useEffect(() => setX(toX(Math.min(w.radiusKm, max), max)), [w.radiusKm, max]);
  const km = toKm(x, max);
  const o = origin(s);
  const places = w.result?.kind === "places" ? w.result.places ?? [] : [];
  const loops = w.result?.kind === "loops" ? w.result.loops ?? [] : [];
  const sel = selectedTrail(s);
  const isWater = s.vehicle === "jetski";

  return (
    <div className="panel wander">
      <header className="hero hero-tight">
        <p className="eyebrow">Wander {o ? `from ${o.name.toLowerCase() === "your location" ? "here" : o.name}` : ""}</p>
        <h1 className="hero-title">How far will you go?</h1>
      </header>

      <div className="radius">
        <div className="radius-read">
          <span className="radius-num">{km}</span>
          <span className="radius-unit">km</span>
          <span className="radius-hint">{hint(km, s.vehicle)}</span>
        </div>
        <Range
          value={x}
          min={0}
          max={100}
          step={0.5}
          label="Wander radius"
          format={() => `${km} km`}
          onInput={(nx) => {
            setX(nx);
            useStore.setState({ wander: { ...useStore.getState().wander, radiusKm: toKm(nx, max) } });
          }}
          onCommit={(nx) => s.setWander({ radiusKm: toKm(nx, max) })}
          ticks={[0, toX(5, max), toX(25, max), 100]}
        />
        <div className="radius-scale" aria-hidden="true">
          <span>1</span>
          <span>5</span>
          <span>25</span>
          <span>{max}</span>
        </div>
      </div>

      <VehicleDial compact />

      {!isWater && (
        <div className="flavours" role="radiogroup" aria-label="What are you in the mood for?">
          {FLAVOURS.map((f) => (
            <button key={f.id} role="radio" aria-checked={w.flavour === f.id} className={`flv ${w.flavour === f.id ? "on" : ""}`} onClick={() => s.setWander({ flavour: f.id })} title={f.hint}>
              {f.label}
            </button>
          ))}
        </div>
      )}

      <div className="wander-tabs">
        <Segmented
          label="Show"
          value={w.tab}
          onChange={(tab) => s.setWander({ tab })}
          options={[
            { id: "places", label: isWater ? "Waters" : "Places", icon: isWater ? "water" : "places" },
            { id: "loops", label: "Loops", icon: "loop" },
          ]}
        />
        <button className="dice" onClick={s.shuffle} aria-label="Surprise me: shuffle">
          <Icon name="dice" size={20} />
          <span>Surprise me</span>
        </button>
      </div>

      {w.status === "loading" && w.tab === "loops" && (
        <div className="cards">
          {[0, 1, 2].map((i) => (
            <TrailSkeleton key={i} i={i} />
          ))}
        </div>
      )}
      {w.status === "loading" && w.tab === "places" && (
        <div className="empty">
          <Spinner label="Looking around" />
        </div>
      )}
      {w.status === "error" && (
        <div className="empty err" role="alert">
          <Icon name={isWater ? "water" : "warn"} size={26} />
          <p>{w.error}</p>
          {isWater && w.tab === "loops" && (
            <button className="btn ghost" onClick={() => s.setWander({ tab: "places" })}>
              Pick a lake first
            </button>
          )}
        </div>
      )}

      {w.status === "done" && w.tab === "places" && (
        <>
          {places.length === 0 && (
            <div className="empty">
              <Icon name="sparkle" size={26} />
              <p>{w.result?.notes?.[0]?.text ?? "Nothing here yet."}</p>
            </div>
          )}
          <ol className="places">
            {places.map((p, i) => (
              <PlaceCard key={p.id} p={p} n={i + 1} onGo={() => s.goToPlace(p)} water={isWater} />
            ))}
          </ol>
        </>
      )}

      {w.status === "done" && w.tab === "loops" && loops.length > 0 && (
        <>
          <div className="cards">
            {loops.map((t, i) => (
              <TrailCard key={t.id} trail={t} index={i} selected={sel?.id === t.id} onSelect={() => s.select(t.id)} />
            ))}
          </div>
          {sel && <TrailDetail trail={sel} />}
        </>
      )}
    </div>
  );
}

function hint(km: number, vehicle: string): string {
  if (vehicle === "walk") return km <= 3 ? "a stroll" : km <= 10 ? "a proper walk" : "a long day on foot";
  if (vehicle === "cycle") return km <= 8 ? "a quick spin" : km <= 30 ? "a good ride" : "a big day out";
  if (vehicle === "jetski") return km <= 5 ? "one lake" : "lakes and rivers";
  return km <= 10 ? "around the corner" : km <= 30 ? "a half-day ride" : km <= 60 ? "a day trip" : "the long road";
}

function PlaceCard({ p, n, onGo, water }: { p: Place; n: number; onGo: () => void; water: boolean }) {
  const k = placeKind(p.kind);
  return (
    <li className="place" style={{ "--t": k.tint, "--d": `${n * 40}ms` } as React.CSSProperties}>
      <button onClick={onGo}>
        <span className="place-n">{n}</span>
        <span className="place-main">
          <span className="place-kind">{k.label}</span>
          <span className="place-name">{p.name}</span>
          <span className="place-why">{p.why}</span>
          {(p.tags ?? []).length > 0 && (
            <span className="place-tags">
              {p.tags!.map((t) => (
                <span key={t}>{t}</span>
              ))}
            </span>
          )}
        </span>
        <span className="place-side">
          <span className="place-d">{p.distance_km < 10 ? p.distance_km.toFixed(1) : Math.round(p.distance_km)} km</span>
          <span className="place-dir">{dir(p.bearing)}</span>
          <span className="place-go">{water ? "Loop it" : "Go"} <Icon name="chevron" size={14} /></span>
        </span>
      </button>
    </li>
  );
}
