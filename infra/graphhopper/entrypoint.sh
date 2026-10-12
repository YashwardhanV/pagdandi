#!/bin/sh
# Serve the graph described by $GH_CONFIG (rendered by `datactl graph`).
# Rewrites paths, bind host and port for the container.
set -e
cfg="${GH_CONFIG:?GH_CONFIG must point to a rendered config.yml}"
[ -f "$cfg" ] || { echo "missing $cfg: run make data first"; exit 1; }
out=/tmp/config.yml
# datactl renders absolute host paths; map the host data dir onto the /data mount.
sed -e "s/bind_host: localhost/bind_host: ${GH_BIND:-0.0.0.0}/"     -e "s#${HOST_DATA_DIR:-/opt/pagdandi/data}#/data#g" "$cfg" > "$out"
if [ -n "${GH_PORT:-}" ]; then sed -i "0,/port: [0-9]*/s//port: ${GH_PORT}/" "$out"; fi
exec java -Xmx"${GH_SERVE_XMX:-2g}" -XX:+UseParallelGC -jar /opt/graphhopper.jar server "$out"
