#!/usr/bin/env bash
# make refresh-graph: weekly OSM refresh with a blue/green swap (SPEC §5).
#  1. re-download the Geofabrik daily extracts, extract, overrides, areas, nodes, overlay
#  2. import into a NEW graph directory and health-check it on a spare port (--verify)
#  3. only then repoint data/build/gh/<c>/current; restart gh-<c>. In prod the API's
#     GH_*_URL lists two instances, so it fails over while one restarts.
set -euo pipefail
. "$(dirname "$0")/env.sh"
REGION="${1:-${REGION:-all}}"
cd "$ROOT/services/api"
go run ./cmd/datactl coverage --region "$REGION"
countries="$(sed -n 's/.*"countries": *\[\(.*\)\].*/\1/p' "$ROOT/data/build/selection.json" | tr -d '" ' | tr ',' ' ' | tr 'A-Z' 'a-z')"
for c in ${countries:-in}; do
  for step in download extract overrides areas nodes overlay; do
    go run ./cmd/datactl "$step" --country "$c" --force
  done
  go run ./cmd/datactl graph --country "$c" --force --verify
  echo "graph for $c rebuilt and verified; restart gh-$c to serve it"
done
