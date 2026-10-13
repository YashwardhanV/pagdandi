import type { Trail } from "../lib/api";
import { deltaDist, deltaTime, duration, km, kmNum, monthLabel } from "../lib/format";
import { KIND_META, trailColor } from "../lib/kinds";
import { Elevation, Meter, SurfaceRibbon } from "../ui/controls";
import { Icon } from "../ui/Icon";

export function TrailCard({ trail, selected, onSelect, index }: { trail: Trail; selected: boolean; onSelect: () => void; index: number }) {
  const color = trailColor(trail);
  const unpaved = ((trail.mix.rough ?? 0) + (trail.mix.dirt ?? 0)) * trail.distance_m;
  const isWater = trail.kind === "open" || trail.kind === "shore" || trail.mix.water === 1;
  const isRef = trail.kind === "usual";
  const also = (trail.also ?? []).map((k) => KIND_META[k]?.label).filter(Boolean);
  return (
    <button
      className={`tcard ${selected ? "on" : ""}`}
      style={{ "--c": color, "--d": `${index * 60}ms` } as React.CSSProperties}
      onClick={onSelect}
      aria-pressed={selected}
    >
      <span className="tcard-strand" aria-hidden="true" />
      <span className="tcard-head">
        <span className="tcard-name">{trail.name}</span>
        <span className="tcard-hi" lang="hi">
          {trail.name_hi}
        </span>
      </span>
      <span className="tcard-tag">{trail.tagline}</span>
      <span className="tcard-nums">
        <span className="big">{duration(trail.duration_s)}</span>
        <span className="mid">{kmNum(trail.distance_m)} km</span>
        {!isRef && trail.kind !== "loop" && !isWater && (
          <span className={`delta ${trail.delta_s > 60 ? "more" : trail.delta_s < -60 ? "less" : ""}`}>
            {deltaTime(trail.delta_s)} · {deltaDist(trail.delta_m)}
          </span>
        )}
      </span>
      <SurfaceRibbon trail={trail} />
      {trail.elevation && trail.elevation.length > 2 && <Elevation trail={trail} color={color} height={30} />}
      <span className="tcard-meta">
        {!isWater && <Meter value={trail.quiet} label="Quiet" color={color} />}
        <Meter value={trail.scenic} label="Scenic" color={color} />
        {trail.climb_m >= 40 && (
          <span className="chip-mini">
            <Icon name="mountain" size={14} /> {Math.round(trail.climb_m)} m
          </span>
        )}
      </span>
      <span className="tcard-badges">
        {unpaved >= 500 && <span className="badge b-kaccha">{km(unpaved)} kaccha</span>}
        {trail.fresh_m >= 200 && (
          <span className="badge b-fresh">
            <Icon name="sparkle" size={12} /> {km(trail.fresh_m)} new since {monthLabel(trail.fresh_since).split(" ")[0]}
          </span>
        )}
        {trail.twisty >= 0.45 && <span className="badge b-twisty">Twisty</span>}
        {also.length > 0 && <span className="badge b-also">also {also.join(", ").toLowerCase()}</span>}
        {(trail.warnings ?? []).some((w) => w.kind === "dark" || w.kind === "monsoon" || w.kind === "rough" || w.kind === "steep") && (
          <span className="badge b-warn">
            <Icon name="warn" size={12} /> heads-up
          </span>
        )}
      </span>
    </button>
  );
}

export function TrailSkeleton({ i }: { i: number }) {
  return (
    <div className="tcard skel" style={{ "--d": `${i * 90}ms` } as React.CSSProperties} aria-hidden="true">
      <span className="sk sk-title" />
      <span className="sk sk-line" />
      <span className="sk sk-nums" />
      <span className="sk sk-bar" />
      <span className="sk sk-line short" />
    </div>
  );
}
