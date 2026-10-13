import { useState } from "react";
import type { Trail } from "../lib/api";
import { clock, duration, km } from "../lib/format";
import { KIND_META, trailColor } from "../lib/kinds";
import { useStore } from "../state/store";
import { Elevation, SurfaceRibbon } from "../ui/controls";
import { Icon, turnIcon } from "../ui/Icon";

const NOTE_ICON: Record<string, string> = {
  unpaved: "warn",
  monsoon: "water",
  dark: "moon",
  steep: "mountain",
  rough: "warn",
  water: "water",
  legal: "info",
  info: "info",
};

const HL_ICON: Record<string, string> = { water: "water", forest: "leaf", wild: "leaf", view: "sparkle", hills: "mountain", twisty: "loop" };

export function TrailDetail({ trail }: { trail: Trail }) {
  const startRide = useStore((s) => s.startRide);
  const [steps, setSteps] = useState(false);
  const color = trailColor(trail);
  const arrive = new Date(Date.now() + trail.duration_s * 1000);
  const total = trail.distance_m || 1;
  const mix = Object.entries(trail.mix)
    .filter(([, v]) => v >= 0.01)
    .sort((a, b) => b[1] - a[1]);
  const highlights = (trail.highlights ?? []).filter((h) => h.label);

  return (
    <article className="detail" style={{ "--c": color } as React.CSSProperties} aria-label={`${trail.name} details`}>
      <header className="detail-head">
        <div>
          <p className="eyebrow">{KIND_META[trail.kind]?.blurb}</p>
          <h3 className="detail-name">{trail.name}</h3>
        </div>
        <div className="detail-eta">
          <span className="big">{duration(trail.duration_s)}</span>
          <span className="sub">
            {km(trail.distance_m)} · arrive {clock(arrive)}
          </span>
        </div>
      </header>

      <div className="detail-actions">
        <button className="btn primary" onClick={() => startRide(trail, false)}>
          <Icon name="nav" size={18} /> Ride this
        </button>
        <button className="btn ghost" onClick={() => startRide(trail, true)}>
          <Icon name="play" size={16} /> Preview
        </button>
      </div>

      {highlights.length > 0 && (
        <section className="detail-sec">
          <h4>Along the way</h4>
          <ol className="timeline">
            {highlights.map((h, i) => (
              <li key={i} style={{ "--at": `${(h.from_m / total) * 100}%` } as React.CSSProperties}>
                <span className={`tl-ico hl-${h.kind}`}>
                  <Icon name={HL_ICON[h.kind] ?? "dot"} size={15} />
                </span>
                <span className="tl-text">{h.label}</span>
                <span className="tl-at">
                  {h.kind === "twisty" ? "" : h.to_m - h.from_m >= 300 ? `${km(h.from_m)} in · ${km(h.to_m - h.from_m)}` : `${km(h.from_m)} in`}
                </span>
              </li>
            ))}
          </ol>
        </section>
      )}

      {(trail.warnings ?? []).length > 0 && (
        <section className="detail-sec">
          <h4>Heads-up</h4>
          <ul className="notes">
            {trail.warnings!.map((w, i) => (
              <li key={i} className={`note note-${w.kind}`}>
                <Icon name={NOTE_ICON[w.kind] ?? "info"} size={16} />
                <span>{w.text}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="detail-sec">
        <h4>Under the wheels</h4>
        <SurfaceRibbon trail={trail} height={12} />
        <div className="mix">
          {mix.map(([k, v]) => (
            <span key={k} className={`mix-i mix-${k}`}>
              <i /> {k} {Math.round(v * 100)}%
            </span>
          ))}
          {trail.backroads_m >= 500 && <span className="mix-i mix-back">village roads & tracks {km(trail.backroads_m)}</span>}
        </div>
      </section>

      {trail.elevation && trail.elevation.length > 2 && (
        <section className="detail-sec">
          <h4>
            Climbs {Math.round(trail.climb_m)} m · descends {Math.round(trail.descent_m)} m
          </h4>
          <Elevation trail={trail} color={color} height={64} axis />
        </section>
      )}

      {(trail.steps ?? []).length > 0 && (
        <section className="detail-sec">
          <button className="linklike" onClick={() => setSteps(!steps)} aria-expanded={steps}>
            {steps ? "Hide" : "Show"} {trail.steps!.length} turns <Icon name={steps ? "up" : "chevron"} size={14} />
          </button>
          {steps && (
            <ol className="steps">
              {trail.steps!.map((s, i) => (
                <li key={i}>
                  <span className="step-ico">
                    <Icon name={turnIcon(s.sign)} size={18} />
                  </span>
                  <span className="step-text">{s.text}</span>
                  <span className="step-d">{s.distance_m > 0 ? km(s.distance_m) : ""}</span>
                </li>
              ))}
            </ol>
          )}
        </section>
      )}
    </article>
  );
}
