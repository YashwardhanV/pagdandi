#!/usr/bin/env sh
# Shared environment for dev scripts. Source it: . scripts/env.sh
ROOT="$(cd "$(dirname "${BASH_SOURCE:-$0}")/.." && pwd)"
TOOLS="$ROOT/data/build/tools"
export ROOT TOOLS
# Bundled tools (portable install on Windows) win over PATH when present.
[ -e "$TOOLS/osmenv/Library/bin/osmium.exe" ] && export OSMIUM="${OSMIUM:-$TOOLS/osmenv/Library/bin/osmium}"
[ -e "$TOOLS/pmtiles/pmtiles.exe" ] && export PMTILES="${PMTILES:-$TOOLS/pmtiles/pmtiles}"
export GH_JAR="${GH_JAR:-$ROOT/data/build/bin/graphhopper-web-11.1.jar}"
export PATH="$TOOLS/osmenv/Library/bin:$TOOLS/pmtiles:$PATH"
# Load .env if present (simple KEY=VALUE lines).
if [ -f "$ROOT/.env" ]; then
  set -a; . "$ROOT/.env"; set +a
fi
