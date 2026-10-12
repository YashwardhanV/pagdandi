#!/usr/bin/env sh
# Shared environment for native (non-Docker) dev. Source it: . scripts/env.sh
ROOT="$(cd "$(dirname "${BASH_SOURCE:-$0}")/.." && pwd)"
TOOLS="$ROOT/data/build/tools"
export ROOT TOOLS
export PGBIN="$TOOLS/pgsql/bin"
export PGDATA_DIR="$ROOT/data/build/pgdata"
export REDIS_DIR="$TOOLS/redis/$(ls "$TOOLS/redis" 2>/dev/null | head -1)"
export OSMIUM="$TOOLS/osmenv/Library/bin/osmium"
export OSM_PY="$TOOLS/osmenv/python"
export PMTILES="$TOOLS/pmtiles/pmtiles"
export GH_JAR="$TOOLS/../bin/graphhopper-web-11.1.jar"
export PATH="$PGBIN:$TOOLS/osmenv/Library/bin:$TOOLS/pmtiles:$PATH"
# Load .env if present (simple KEY=VALUE lines).
if [ -f "$ROOT/.env" ]; then
  set -a; . "$ROOT/.env"; set +a
fi
