import { useId, type ReactNode } from "react";
import type { Trail, VehicleId } from "../lib/api";
import { VEHICLE_ORDER } from "../lib/kinds";
import { useStore } from "../state/store";
import { Icon } from "./Icon";

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
  size = "md",
}: {
  value: T;
  options: { id: T; label: ReactNode; icon?: string }[];
  onChange: (v: T) => void;
  label: string;
  size?: "md" | "lg";
}) {
  const idx = Math.max(0, options.findIndex((o) => o.id === value));
  return (
    <div className={`seg seg-${size}`} role="tablist" aria-label={label} style={{ "--n": options.length, "--i": idx } as React.CSSProperties}>
      <span className="seg-pill" aria-hidden="true" />
      {options.map((o) => (
        <button key={o.id} role="tab" aria-selected={o.id === value} className={o.id === value ? "on" : ""} onClick={() => onChange(o.id)}>
          {o.icon && <Icon name={o.icon} size={18} />}
          <span>{o.label}</span>
        </button>
      ))}
    </div>
  );
}

export function VehicleDial({ compact = false }: { compact?: boolean }) {
  const vehicle = useStore((s) => s.vehicle);
  const setVehicle = useStore((s) => s.setVehicle);
  const vehicles = useStore((s) => s.meta?.vehicles);
  const label = (id: VehicleId) => vehicles?.find((v) => v.id === id)?.label ?? id;
  return (
    <div className={`dial ${compact ? "dial-compact" : ""}`} role="radiogroup" aria-label="How are you travelling?">
      {VEHICLE_ORDER.map((id) => (
        <button key={id} role="radio" aria-checked={vehicle === id} className={vehicle === id ? "on" : ""} onClick={() => setVehicle(id)} title={label(id)}>
          <span className="dial-ico">
            <Icon name={id} size={compact ? 22 : 26} />
          </span>
          <span className="dial-label">{label(id)}</span>
        </button>
      ))}
    </div>
  );
}

/** A range input with a value bubble and ticks. */
export function Range({
  value,
  min,
  max,
  step = 1,
  onInput,
  onCommit,
  label,
  format,
  ticks,
}: {
  value: number;
  min: number;
  max: number;
  step?: number;
  onInput: (v: number) => void;
  onCommit: (v: number) => void;
  label: string;
  format: (v: number) => string;
  ticks?: number[];
}) {
  const id = useId();
  const pct = ((value - min) / (max - min)) * 100;
  return (
    <div className="range" style={{ "--p": `${pct}%` } as React.CSSProperties}>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <input
        id={id}
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        aria-valuetext={format(value)}
        onChange={(e) => onInput(Number(e.target.value))}
        onPointerUp={(e) => onCommit(Number((e.target as HTMLInputElement).value))}
        onKeyUp={(e) => onCommit(Number((e.target as HTMLInputElement).value))}
      />
      {ticks && (
        <div className="range-ticks" aria-hidden="true">
          {ticks.map((t) => (
            <span key={t} style={{ left: `${((t - min) / (max - min)) * 100}%` }} />
          ))}
        </div>
      )}
    </div>
  );
}

/** Surface ribbon: the route as a bar coloured by what's under the wheels. */
export function SurfaceRibbon({ trail, height = 8 }: { trail: Trail; height?: number }) {
  const total = trail.distance_m || 1;
  return (
    <div className="ribbon" style={{ height }} aria-label={surfaceSummary(trail)} role="img">
      {trail.surfaces.map((s, i) => (
        <span key={i} className={`rb rb-${s.kind}`} style={{ left: `${(s.from_m / total) * 100}%`, width: `${Math.max(0.6, ((s.to_m - s.from_m) / total) * 100)}%` }} />
      ))}
      {(trail.fresh_spans ?? []).map((s, i) => (
        <span key={`f${i}`} className="rb-fresh" style={{ left: `${(s.from_m / total) * 100}%`, width: `${Math.max(0.8, ((s.to_m - s.from_m) / total) * 100)}%` }} />
      ))}
    </div>
  );
}

export function surfaceSummary(t: Trail): string {
  const parts = Object.entries(t.mix)
    .filter(([, v]) => v >= 0.01)
    .sort((a, b) => b[1] - a[1])
    .map(([k, v]) => `${Math.round(v * 100)}% ${k}`);
  return `Surface: ${parts.join(", ")}`;
}

/** Elevation sparkline with a soft fill in the trail colour. */
export function Elevation({ trail, color, height = 40, axis = false }: { trail: Trail; color: string; height?: number; axis?: boolean }) {
  const id = useId().replace(/:/g, "");
  const e = trail.elevation;
  if (!e || e.length < 2) return null;
  const w = 300;
  const min = Math.min(...e.map((p) => p[1]));
  const max = Math.max(...e.map((p) => p[1]));
  const span = Math.max(25, max - min);
  const total = e[e.length - 1][0] || 1;
  const pts = e.map((p) => [(p[0] / total) * w, height - 3 - ((p[1] - min) / span) * (height - 8)]);
  const line = pts.map((p, i) => `${i ? "L" : "M"}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join("");
  return (
    <div className="elev">
      <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" width="100%" height={height} role="img" aria-label={`Elevation ${Math.round(min)}–${Math.round(max)} m, climbs ${trail.climb_m} m`}>
        <defs>
          <linearGradient id={`g${id}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor={color} stopOpacity="0.38" />
            <stop offset="1" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        <path d={`${line}L${w},${height}L0,${height}Z`} fill={`url(#g${id})`} />
        <path d={line} fill="none" stroke={color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
      </svg>
      {axis && (
        <div className="elev-axis">
          <span>{Math.round(min)} m</span>
          <span>{Math.round(max)} m</span>
        </div>
      )}
    </div>
  );
}

/** Five-segment meter for 0..1 scores. */
export function Meter({ value, label, color }: { value: number; label: string; color: string }) {
  const n = Math.round(value * 5);
  return (
    <span className="meter" aria-label={`${label}: ${Math.round(value * 100)}%`} role="img" style={{ "--c": color } as React.CSSProperties}>
      <span className="meter-label">{label}</span>
      <span className="meter-bars" aria-hidden="true">
        {[0, 1, 2, 3, 4].map((i) => (
          <i key={i} className={i < n ? "on" : ""} />
        ))}
      </span>
    </span>
  );
}

export function Spinner({ label = "Loading" }: { label?: string }) {
  return <span className="spinner" role="status" aria-label={label} />;
}
