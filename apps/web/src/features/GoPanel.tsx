import { useEffect, useRef, useState } from "react";
import { haversine } from "../lib/geo";
import { useGems } from "../lib/hooks";
import { placeKind } from "../lib/kinds";
import { origin, selectedTrail, useStore } from "../state/store";
import { Range, VehicleDial } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { TrailCard, TrailSkeleton } from "./TrailCard";
import { TrailDetail } from "./TrailDetail";

export function GoPanel() {
  const to = useStore((s) => s.to);
  return to ? <Trip /> : <Home />;
}

function greeting(): string {
  const h = new Date().getHours();
  if (h < 5) return "Night owl? The roads are empty.";
  if (h < 11) return "Good morning. Where to?";
  if (h < 16) return "Where are we going today?";
  if (h < 19) return "Golden hour's coming. Where to?";
  return "Evening ride? Pick a place.";
}

function Home() {
  const openSearch = useStore((s) => s.openSearch);
  const setTo = useStore((s) => s.setTo);
  const recent = useStore((s) => s.recent);
  const me = useStore((s) => s.me);
  const meta = useStore((s) => s.meta);
  const gems = useGems();
  const from = me ?? meta?.region.center;
  const featured = gems
    .filter((g) => g.featured)
    .map((g) => ({ g, d: from ? haversine(from, g) : 0 }))
    .sort((a, b) => a.d - b.d)
    .slice(0, 12);

  return (
    <div className="panel home">
      <header className="hero">
        <p className="eyebrow">Pagdandi · {meta?.region.name ?? "Indore"}</p>
        <h1 className="hero-title">{greeting()}</h1>
        <p className="hero-sub">Every road has a secret twin. We'll show you the usual way, and the other ways.</p>
      </header>
      <button className="searchbox" onClick={() => openSearch("to")}>
        <Icon name="search" size={20} />
        <span>Search a place, or long-press the map</span>
      </button>
      <VehicleDial />
      {featured.length > 0 && (
        <section className="shelf">
          <h2 className="shelf-title">Where Indore rides</h2>
          <div className="shelf-row">
            {featured.map(({ g, d }) => (
              <button
                key={g.id}
                className="gem-chip"
                style={{ "--t": placeKind(g.kind).tint } as React.CSSProperties}
                onClick={() => setTo({ name: g.name, sub: placeKind(g.kind).label, at: g.access ?? { lat: g.lat, lng: g.lng }, kind: g.kind })}
              >
                <span className="gem-kind">{placeKind(g.kind).label}</span>
                <span className="gem-name">{g.name}</span>
                {d > 0 && <span className="gem-d">{Math.round(d / 1000)} km</span>}
              </button>
            ))}
          </div>
        </section>
      )}
      {recent.length > 0 && (
        <section className="shelf">
          <h2 className="shelf-title">Recent</h2>
          <ul className="list">
            {recent.slice(0, 5).map((r) => (
              <li key={r.name + r.at.lat}>
                <button className="row" onClick={() => setTo(r)}>
                  <Icon name="clock" size={18} />
                  <span className="row-main">
                    <span className="row-title">{r.name}</span>
                    {r.sub && <span className="row-sub">{r.sub}</span>}
                  </span>
                  <Icon name="chevron" size={16} />
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function Trip() {
  const s = useStore();
  const o = origin(s);
  const vehicle = s.meta?.vehicles.find((v) => v.id === s.vehicle);
  const result = s.go.result;
  const trails = result?.trails ?? [];
  const sel = selectedTrail(s);
  const [stray, setStrayLocal] = useState<number>(s.stray ?? result?.stray_km ?? 5);
  useEffect(() => {
    if (result) setStrayLocal(s.stray ?? result.stray_km);
  }, [result, s.stray]);
  const cards = useRef<HTMLDivElement>(null);

  // Keep the selected card in view.
  useEffect(() => {
    const el = cards.current?.querySelector<HTMLElement>(".tcard.on");
    el?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "center" });
  }, [s.selected]);

  return (
    <div className="panel trip">
      <div className="trip-ends">
        <div className="ends-line" aria-hidden="true">
          <i className="e-from" />
          <i className="e-path" />
          <i className="e-to" />
        </div>
        <div className="ends-fields">
          <button className="end" onClick={() => s.openSearch("from")}>
            <span className="end-label">From</span>
            <span className="end-name">{o?.name ?? "Choose start"}</span>
          </button>
          <button className="end" onClick={() => s.openSearch("to")}>
            <span className="end-label">To</span>
            <span className="end-name">{s.to?.name}</span>
          </button>
        </div>
        <div className="ends-tools">
          <button className="icon-btn" onClick={s.swap} aria-label="Swap start and destination">
            <Icon name="swap" size={18} />
          </button>
          <button className="icon-btn" onClick={s.clearTrip} aria-label="Clear trip">
            <Icon name="close" size={18} />
          </button>
        </div>
      </div>

      <VehicleDial compact />

      {!vehicle?.water && (
        <div className="stray">
          <div className="stray-head">
            <span className="stray-q">How far will you stray?</span>
            <span className="stray-v">
              +{stray} km{s.stray == null && <em> auto</em>}
            </span>
          </div>
          <Range
            value={stray}
            min={0}
            max={40}
            step={1}
            label="Detour budget in kilometres"
            format={(v) => `${v} km extra`}
            onInput={setStrayLocal}
            onCommit={(v) => s.setStray(v)}
            ticks={[0, 10, 20, 30, 40]}
          />
        </div>
      )}

      {s.go.status === "loading" && (
        <div className="cards" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <TrailSkeleton key={i} i={i} />
          ))}
        </div>
      )}

      {s.go.status === "error" && (
        <div className="empty err" role="alert">
          <Icon name={s.go.code === "different_water" || s.go.code === "no_water" ? "water" : "warn"} size={26} />
          <p>{s.go.error}</p>
          {s.vehicle === "jetski" && <p className="muted">Tip: in Wander → Places, pick a lake to ride on.</p>}
          <button className="btn ghost" onClick={() => void s.plan()}>
            Try again
          </button>
        </div>
      )}

      {s.go.status === "done" && trails.length > 0 && (
        <>
          <div className="cards-head">
            <h2>
              {trails.length === 1 ? "One way" : `${trails.length} ways`} to {s.to?.name}
            </h2>
            {result && <span className="muted small">planned in {result.took_ms} ms</span>}
          </div>
          <div className="cards" ref={cards} role="list">
            {trails.map((t, i) => (
              <TrailCard key={t.id} trail={t} index={i} selected={sel?.id === t.id} onSelect={() => s.select(t.id)} />
            ))}
          </div>
          {(result?.notes ?? []).map((n, i) => (
            <p key={i} className="note-inline">
              <Icon name="info" size={15} /> {n.text}
            </p>
          ))}
          {sel && <TrailDetail trail={sel} />}
        </>
      )}
    </div>
  );
}
