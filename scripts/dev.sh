#!/usr/bin/env bash
# make dev: api, worker, GraphHopper and web with hot reload.
# `scripts/dev.sh stop` stops everything it started.
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

if [ "${PAGDANDI_RUNTIME:-}" != "native" ] && docker info >/dev/null 2>&1; then
  docker compose -f "$ROOT/infra/docker-compose.yml" --profile dev up -d postgres redis
else
  "$ROOT/scripts/services-up.sh"
fi

echo "starting:"
for c in in se; do
  if [ -f "$ROOT/data/build/gh/$c/config.yml" ]; then
    start "gh-$c" "$ROOT/scripts/gh-serve.sh" "$c"
  else
    echo "  gh-$c skipped (no graph yet; run make data)"
  fi
done

cd "$ROOT/services/api"
if command -v air >/dev/null; then
  start api air -c .air.api.toml
  start worker air -c .air.worker.toml
else
  start api go run ./cmd/api
  start worker go run ./cmd/worker
fi
cd "$ROOT/apps/web"
start web npm run dev -- --host

trap 'echo; stop_all; exit 0' INT TERM
echo
echo "  web  → http://localhost:5173"
echo "  api  → http://localhost:8080/v1"
echo "  gh   → http://localhost:8989 (in), :8990 (se)"
echo "Ctrl-C stops everything. Sign-in links are printed in data/build/logs/api.log."
while true; do sleep 3600; done
