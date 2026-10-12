#!/usr/bin/env sh
# Shared environment for native (non-Docker) dev. Source it: . scripts/env.sh
ROOT="$(cd "$(dirname "${BASH_SOURCE:-$0}")/.." && pwd)"
TOOLS="$ROOT/data/build/tools"
export ROOT TOOLS
export PGBIN="$TOOLS/pgsql/bin"
export PGDATA_DIR="$ROOT/data/build/pgdata"
export REDIS_DIR="$TOOLS/redis/$(ls "$TOOLS/redis" 2>/dev/null | head -1)"
# Bundled tools (native Windows runtime) are used only when present; otherwise PATH.
[ -e "$TOOLS/osmenv/Library/bin/osmium.exe" ] && export OSMIUM="${OSMIUM:-$TOOLS/osmenv/Library/bin/osmium}"
[ -e "$TOOLS/osmenv/python.exe" ] && export OSM_PY="${OSM_PY:-$TOOLS/osmenv/python}"
[ -e "$TOOLS/pmtiles/pmtiles.exe" ] && export PMTILES="${PMTILES:-$TOOLS/pmtiles/pmtiles}"
export OSM_PY="${OSM_PY:-python3}"
export GH_JAR="${GH_JAR:-$ROOT/data/build/bin/graphhopper-web-11.1.jar}"
export PATH="$PGBIN:$TOOLS/osmenv/Library/bin:$TOOLS/pmtiles:$PATH"
# Load .env if present (simple KEY=VALUE lines).
if [ -f "$ROOT/.env" ]; then
  set -a; . "$ROOT/.env"; set +a
fi
