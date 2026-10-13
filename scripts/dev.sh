#!/usr/bin/env bash
# make dev: GraphHopper, the API and the web app. `scripts/dev.sh stop` stops them.
set -uo pipefail
. "$(dirname "$0")/env.sh"
RUN="$ROOT/data/build/run"
LOGS="$ROOT/data/build/logs"
mkdir -p "$RUN" "$LOGS"

stop_all() {
  for f in "$RUN"/*.pid; do
    [ -f "$f" ] || continue
    pid="$(cat "$f")"
    kill "$pid" 2>/dev/null || true
    if command -v taskkill >/dev/null; then taskkill //F //T //PID "$pid" >/dev/null 2>&1 || true; fi
    rm -f "$f"
  done
}
if [ "${1:-}" = "stop" ]; then stop_all; echo "stopped"; exit 0; fi

start() { # name, command...
  local name="$1"; shift
  "$@" >"$LOGS/$name.log" 2>&1 &
  echo $! >"$RUN/$name.pid"
  echo "  $name → data/build/logs/$name.log"
}

[ -f "$ROOT/data/build/gh/config.yml" ] || { echo "No graph yet. Run: make data"; exit 1; }
echo "starting:"
start gh "$ROOT/scripts/gh-serve.sh"
(cd "$ROOT/services/api" && go build -o bin/ ./cmd/api) || exit 1
start api "$ROOT/services/api/bin/api"
cd "$ROOT/apps/web"
start web npm run dev -- --host

trap 'echo; stop_all; exit 0' INT TERM
echo
echo "  web → http://localhost:5173   (GraphHopper takes ~20 s to load the graph)"
echo "  api → http://localhost:8080/v1/meta"
echo "Ctrl-C stops everything."
while true; do sleep 3600; done
