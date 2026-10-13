import { useEffect, useState } from "react";
import { GoPanel } from "../features/GoPanel";
import { About, LayerSheet, SearchOverlay, Toast } from "../features/Overlays";
import { Ride } from "../features/Ride";
import { WanderPanel } from "../features/WanderPanel";
import { MapView } from "../map/MapView";
import { origin, useStore } from "../state/store";
import { Segmented, Spinner } from "../ui/controls";
import { Icon, Logo } from "../ui/Icon";
import { Sheet } from "../ui/Sheet";

export function App() {
  const init = useStore((s) => s.init);
  const mode = useStore((s) => s.mode);
  const meta = useStore((s) => s.meta);
  const metaError = useStore((s) => s.metaError);
  const tickTheme = useStore((s) => s.tickTheme);
  const [snap, setSnap] = useState<"peek" | "half" | "full">("half");
  const scrollKey = useStore((s) => `${s.mode}|${s.to?.name ?? ""}|${s.go.status === "loading"}|${s.wander.tab}`);

  useEffect(() => {
    void init();
    const t = setInterval(tickTheme, 60_000);
    return () => clearInterval(t);
  }, [init, tickTheme]);

  // Results deserve room; home and wander start at half.
  useEffect(() => {
    return useStore.subscribe((s, p) => {
      if (s.go.status === "done" && p.go.status !== "done") setSnap("half");
      if (s.mode !== p.mode && s.mode !== "ride") setSnap("half");
    });
  }, []);

  if (!meta) {
    return (
      <div className="boot">
        <Logo size={64} />
        <h1 className="wordmark big">
          <span lang="hi">पगडंडी</span>
        </h1>
        {metaError ? (
          <>
            <p className="muted">{metaError}</p>
            <button className="btn primary" onClick={() => void init()}>
              Try again
            </button>
          </>
        ) : (
          <Spinner label="Loading Pagdandi" />
        )}
      </div>
    );
  }

  return (
    <div className="app" data-mode={mode}>
      <MapView />
      {mode === "ride" ? (
        <Ride />
      ) : (
        <>
          <TopBar />
          <MapButtons />
          <Sheet snap={snap} onSnap={setSnap} header={<ModeSwitch />} scrollKey={scrollKey}>
            {mode === "go" ? <GoPanel /> : <WanderPanel />}
          </Sheet>
        </>
      )}
      <SearchOverlay />
      <LayerSheet />
      <About />
      <Toast />
    </div>
  );
}

function ModeSwitch() {
  const mode = useStore((s) => s.mode);
  const setMode = useStore((s) => s.setMode);
  return (
    <Segmented
      size="lg"
      label="Mode"
      value={mode === "ride" ? "go" : mode}
      onChange={setMode}
      options={[
        { id: "go", label: "Go", icon: "nav" },
        { id: "wander", label: "Wander", icon: "sparkle" },
      ]}
    />
  );
}

function TopBar() {
  const s = useStore();
  const o = origin(s);
  return (
    <header className="topbar">
      <button className="brand" onClick={() => s.setAbout(true)} aria-label="About Pagdandi">
        <Logo size={34} />
        <span className="wordmark">
          <span lang="hi">पगडंडी</span>
        </span>
      </button>
      <button className="pill" onClick={() => s.openSearch("to")}>
        <Icon name="search" size={18} />
        {s.to ? (
          <span className="pill-trip">
            <span className="pill-from">{o?.isMe ? "You" : o?.name}</span>
            <Icon name="chevron" size={12} />
            <span className="pill-to">{s.to.name}</span>
          </span>
        ) : (
          <span className="pill-ph">Where to?</span>
        )}
      </button>
    </header>
  );
}

function MapButtons() {
  const s = useStore();
  return (
    <div className="mapbtns">
      <button className="fab" onClick={() => s.setLayers(true)} aria-label="Map layers">
        <Icon name="layers" size={20} />
      </button>
      <button className={`fab ${s.terrain3d ? "on" : ""}`} onClick={s.toggle3d} aria-label="3D terrain" aria-pressed={s.terrain3d}>
        <Icon name="cube" size={20} />
      </button>
      <button className={`fab ${s.locating ? "busy" : ""}`} onClick={() => s.locate()} aria-label="My location">
        <Icon name="locate" size={20} />
      </button>
    </div>
  );
}
