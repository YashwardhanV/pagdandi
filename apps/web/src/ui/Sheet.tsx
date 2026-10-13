import { useEffect, useRef, useState, type ReactNode } from "react";

type Snap = "peek" | "half" | "full";
const HEIGHTS: Record<Snap, number> = { peek: 0.3, half: 0.56, full: 0.9 };

/**
 * Bottom sheet on phones (drag the handle between peek / half / full), a
 * floating side panel on wide screens. Content scrolls inside.
 */
export function Sheet({ children, snap, onSnap, header, scrollKey }: { children: ReactNode; snap: Snap; onSnap: (s: Snap) => void; header?: ReactNode; scrollKey?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  useEffect(() => {
    body.current?.scrollTo({ top: 0, behavior: "smooth" });
  }, [scrollKey]);
  const drag = useRef<{ y: number; h: number } | null>(null);
  const [dragH, setDragH] = useState<number | null>(null);
  const [vh, setVh] = useState(() => window.innerHeight);

  useEffect(() => {
    const on = () => setVh(window.innerHeight);
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);

  const height = dragH ?? HEIGHTS[snap] * vh;

  const onDown = (e: React.PointerEvent) => {
    drag.current = { y: e.clientY, h: height };
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  };
  const onMove = (e: React.PointerEvent) => {
    if (!drag.current) return;
    const h = Math.max(120, Math.min(vh * 0.94, drag.current.h + (drag.current.y - e.clientY)));
    setDragH(h);
  };
  const onUp = () => {
    if (!drag.current) return;
    const h = dragH ?? drag.current.h;
    drag.current = null;
    setDragH(null);
    const frac = h / vh;
    const best = (Object.keys(HEIGHTS) as Snap[]).reduce((a, b) => (Math.abs(HEIGHTS[b] - frac) < Math.abs(HEIGHTS[a] - frac) ? b : a));
    onSnap(best);
  };
  const cycle = () => onSnap(snap === "peek" ? "half" : snap === "half" ? "full" : "half");

  return (
    <section
      ref={ref}
      className={`sheet ${dragH != null ? "dragging" : ""}`}
      data-sheet
      data-snap={snap}
      style={{ "--sheet-h": `${Math.round(height)}px` } as React.CSSProperties}
    >
      <div className="sheet-grip" onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}>
        <button className="grip" aria-label={snap === "full" ? "Shrink panel" : "Expand panel"} onClick={cycle} />
        {header}
      </div>
      <div className="sheet-body" ref={body}>
        {children}
      </div>
    </section>
  );
}
