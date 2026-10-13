#!/usr/bin/env bash
# Serve the built GraphHopper graph on :8989.
set -euo pipefail
. "$(dirname "$0")/env.sh"
cfg="$ROOT/data/build/gh/config.yml"
[ -f "$cfg" ] || { echo "no graph yet: run make data"; exit 1; }
exec java -Xmx"${GH_SERVE_XMX:-2g}" -XX:+UseParallelGC -jar "$GH_JAR" server "$cfg"
