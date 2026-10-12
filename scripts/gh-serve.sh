#!/usr/bin/env bash
# Serve a built GraphHopper graph: gh-serve.sh in|se|fixture
set -euo pipefail
. "$(dirname "$0")/env.sh"
which="${1:-in}"
case "$which" in
  fixture) cfg="$ROOT/data/build/fixture/gh/in/config.yml" ;;
  *)       cfg="$ROOT/data/build/gh/$which/config.yml" ;;
esac
[ -f "$cfg" ] || { echo "no graph for '$which' yet: run make data"; exit 1; }
exec java -Xmx"${GH_SERVE_XMX:-2g}" -XX:+UseParallelGC -jar "$GH_JAR" server "$cfg"
