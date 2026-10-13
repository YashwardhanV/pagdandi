import { useEffect, useRef, useState } from "react";
import { api, type SearchPlace } from "../lib/api";
import { haversine } from "../lib/geo";
import { useGems } from "../lib/hooks";
import { KIND_META, placeKind } from "../lib/kinds";
import { origin, useStore, type Endpoint } from "../state/store";
import { Segmented, Spinner } from "../ui/controls";
import { Icon, Logo } from "../ui/Icon";

export function SearchOverlay() {
  const which = useStore((s) => s.search);
  const close = () => useStore.getState().openSearch(null);
  const [q, setQ] = useState("");
  const [res, setRes] = useState<SearchPlace[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const gems = useGems();
  const recent = useStore((s) => s.recent);
  const near = useStore((s) => origin(s)?.at ?? null);

  useEffect(() => {
    if (which) {
      setQ("");
      setRes([]);
      setTimeout(() => input.current?.focus(), 30);
    }
  }, [which]);

  useEffect(() => {
    if (!which || q.trim().length < 2) {
      setRes([]);
      return;
    }
    const ac = new AbortController();
    const t = setTimeout(async () => {
      setBusy(true);
      setErr(null);
      try {
        const r = await api.search(q, near, ac.signal);
        setRes(r.places);
      } catch (e) {
        if ((e as Error).name !== "AbortError") setErr((e as Error).message);
      } finally {
        if (!ac.signal.aborted) setBusy(false);
      }
    }, 260);
    return () => {
      clearTimeout(t);
      ac.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, which]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && which && close();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [which]);

  if (!which) return null;

  const pick = (e: Endpoint) => {
    const st = useStore.getState();
    if (which === "to") st.setTo(e);
    else st.setFrom(e);
  };

  // Local gems answer instantly while the geocoder thinks.
  const ql = q.trim().toLowerCase();
  const local: SearchPlace[] =
    ql.length >= 2
      ? gems
          .filter((g) => g.name.toLowerCase().includes(ql))
          .slice(0, 5)
          .map((g) => ({ id: g.id, name: g.name, label: placeKind(g.kind).label, kind: g.kind, lat: (g.access ?? g).lat, lng: (g.access ?? g).lng, source: "gem", inside: true }))
      : [];
  const shown = res.length ? res : local;

  const featured = gems
    .filter((g) => g.featured)
    .map((g) => ({ g, d: near ? haversine(near, g) : 0 }))
    .sort((a, b) => a.d - b.d)
    .slice(0, 10);

  return (
    <div className="overlay search" role="dialog" aria-modal="true" aria-label={which === "to" ? "Where to?" : "Start from"}>
      <div className="search-bar">
        <button className="icon-btn" onClick={close} aria-label="Close search">
          <Icon name="back" size={22} />
        </button>
        <input
          ref={input}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={which === "to" ? "Where to? A lake, a fort, a chai stall…" : "Start from…"}
          aria-label="Search places"
          enterKeyHint="search"
          autoComplete="off"
        />
        {busy && <Spinner label="Searching" />}
        {q && !busy && (
          <button className="icon-btn" onClick={() => setQ("")} aria-label="Clear">
            <Icon name="close" size={18} />
          </button>
        )}
      </div>
      <div className="search-body">
        {which === "from" && (
          <button
            className="row"
            onClick={() => {
              const st = useStore.getState();
              st.setFrom(null);
              st.locate();
            }}
          >
            <Icon name="locate" size={18} />
            <span className="row-main">
              <span className="row-title">My location</span>
              <span className="row-sub">Use GPS</span>
            </span>
          </button>
        )}
        {err && <p className="muted pad">{err}</p>}
        {shown.length > 0 && (
          <ul className="list">
            {shown.map((p) => (
              <li key={p.id + p.lat}>
                <button
                  className="row"
                  disabled={!p.inside}
                  onClick={() => pick({ name: p.name, sub: p.label || placeKind(p.kind).label, at: { lat: p.lat, lng: p.lng }, kind: p.kind })}
                >
                  <Icon name={p.source === "gem" ? "sparkle" : "pin"} size={18} />
                  <span className="row-main">
                    <span className="row-title">{p.name}</span>
                    <span className="row-sub">{p.inside ? p.label || placeKind(p.kind).label : "Outside the Indore area for now"}</span>
                  </span>
                  {near && <span className="row-d">{Math.round(haversine(near, p) / 1000)} km</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
        {q.trim().length < 2 && (
          <>
            {recent.length > 0 && (
              <section className="shelf">
                <h2 className="shelf-title">Recent</h2>
                <ul className="list">
                  {recent.map((r) => (
                    <li key={r.name + r.at.lat}>
                      <button className="row" onClick={() => pick(r)}>
                        <Icon name="clock" size={18} />
                        <span className="row-main">
                          <span className="row-title">{r.name}</span>
                          {r.sub && <span className="row-sub">{r.sub}</span>}
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            <section className="shelf">
              <h2 className="shelf-title">Places Indore rides to</h2>
              <ul className="list">
                {featured.map(({ g, d }) => (
                  <li key={g.id}>
                    <button className="row" onClick={() => pick({ name: g.name, sub: placeKind(g.kind).label, at: g.access ?? { lat: g.lat, lng: g.lng }, kind: g.kind })}>
                      <span className="kind-dot" style={{ background: placeKind(g.kind).tint }} />
                      <span className="row-main">
                        <span className="row-title">{g.name}</span>
                        <span className="row-sub">{placeKind(g.kind).label}</span>
                      </span>
                      {d > 0 && <span className="row-d">{Math.round(d / 1000)} km</span>}
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          </>
        )}
      </div>
    </div>
  );
}

export function LayerSheet() {
  const s = useStore();
  if (!s.layers) return null;
  const date = s.meta?.data.osm_date ? new Date(s.meta.data.osm_date).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" }) : null;
  return (
    <div className="overlay pop" role="dialog" aria-modal="true" aria-label="Map layers" onClick={() => s.setLayers(false)}>
      <div className="pop-card" onClick={(e) => e.stopPropagation()}>
        <header className="pop-head">
          <h2>Map</h2>
          <button className="icon-btn" onClick={() => s.setLayers(false)} aria-label="Close">
            <Icon name="close" size={18} />
          </button>
        </header>
        <p className="pop-label">Mood</p>
        <Segmented
          label="Mood"
          value={s.theme}
          onChange={s.setTheme}
          options={[
            { id: "auto", label: "Auto", icon: "auto" },
            { id: "dusk", label: "Dusk", icon: "moon" },
            { id: "day", label: "Day", icon: "sun" },
          ]}
        />
        <p className="pop-hint">Auto switches to Dusk at Indore's sunset.</p>
        <p className="pop-label">Base</p>
        <div className="bases">
          {(["atlas", "satellite"] as const).map((b) => (
            <button key={b} className={`base base-${b} ${s.base === b ? "on" : ""}`} onClick={() => s.setBase(b)} aria-pressed={s.base === b}>
              <span className="base-swatch" aria-hidden="true" />
              <span>{b === "atlas" ? "Atlas" : "Satellite"}</span>
            </button>
          ))}
        </div>
        <p className="pop-label">Show</p>
        <label className="toggle">
          <input type="checkbox" checked={s.terrain3d} onChange={s.toggle3d} />
          <span className="toggle-ui" aria-hidden="true" />
          <span className="toggle-text">
            <b>3D terrain</b>
            <small>Tilt the map to see the hills and ghats</small>
          </span>
        </label>
        <label className="toggle">
          <input type="checkbox" checked={s.showFresh} onChange={s.toggleFresh} />
          <span className="toggle-ui" aria-hidden="true" />
          <span className="toggle-text">
            <b>New on the map</b>
            <small>Roads added to OpenStreetMap in the last {s.meta?.data.fresh_months ?? 18} months</small>
          </span>
        </label>
        <p className="pop-foot">
          Map data © OpenStreetMap contributors{date ? `, as of ${date}` : ""}. Satellite © Esri. Terrain © Mapzen/AWS.
        </p>
      </div>
    </div>
  );
}

export function About() {
  const s = useStore();
  if (!s.about) return null;
  const kinds = ["usual", "shortest", "kaccha", "quiet", "scenic", "fresh", "loop", "open", "shore"] as const;
  return (
    <div className="overlay pop" role="dialog" aria-modal="true" aria-label="About Pagdandi" onClick={() => s.setAbout(false)}>
      <div className="pop-card about" onClick={(e) => e.stopPropagation()}>
        <header className="pop-head">
          <div className="about-brand">
            <Logo size={40} />
            <div>
              <h2 className="wordmark">
                <span lang="hi">पगडंडी</span> Pagdandi
              </h2>
              <p className="muted small">a narrow path worn by people walking it</p>
            </div>
          </div>
          <button className="icon-btn" onClick={() => s.setAbout(false)} aria-label="Close">
            <Icon name="close" size={18} />
          </button>
        </header>
        <p>
          Maps show you the road everyone takes. Pagdandi shows that one too, and then the others: the shortcut through the village, the kaccha track, the quiet
          lane, the lakeside detour, the road that only just appeared on the map.
        </p>
        <ul className="legend">
          {kinds.map((k) => (
            <li key={k} style={{ "--c": `var(--k-${k})` } as React.CSSProperties}>
              <i />
              <b>{KIND_META[k].label}</b>
              <span>{KIND_META[k].blurb}</span>
            </li>
          ))}
        </ul>
        <p className="warn-box">
          <Icon name="warn" size={16} /> Unpaved and new roads change with the seasons. Labels are honest guesses from OpenStreetMap. Ride at your own judgement;
          expressways closed to two-wheelers, private land and military areas are always avoided.
        </p>
        <p className="muted small">
          Long-press (or right-click) the map to go anywhere. Built on OpenStreetMap, GraphHopper and Protomaps.
        </p>
      </div>
    </div>
  );
}

export function Toast() {
  const t = useStore((s) => s.toast);
  if (!t) return null;
  return (
    <div className={`toast toast-${t.tone}`} role="status">
      {t.tone === "warn" && <Icon name="warn" size={16} />}
      {t.text}
    </div>
  );
}
