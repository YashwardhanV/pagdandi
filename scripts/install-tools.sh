#!/usr/bin/env bash
# Install native data tools into data/build/tools (idempotent). On Linux/macOS
# the package-manager versions are used when present; on Windows everything is
# portable and needs no admin rights (see docs/DECISIONS.md).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$ROOT/data/build/tools"
mkdir -p "$T" "$ROOT/data/build/bin"

GH_VER=11.1
if [ ! -f "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar" ]; then
  curl -fsSL -o "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar" \
    "https://github.com/graphhopper/graphhopper/releases/download/$GH_VER/graphhopper-web-$GH_VER.jar"
fi
command -v java >/dev/null || echo "WARNING: install Java 17+ (e.g. Temurin) for GraphHopper"
command -v go >/dev/null || echo "WARNING: install Go 1.23+"
command -v node >/dev/null || echo "WARNING: install Node LTS"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    if [ ! -x "$T/pmtiles/pmtiles.exe" ]; then
      curl -fsSL -o "$T/pmtiles.zip" https://github.com/protomaps/go-pmtiles/releases/download/v1.31.2/go-pmtiles_1.31.2_Windows_x86_64.zip
      mkdir -p "$T/pmtiles" && unzip -q -o "$T/pmtiles.zip" -d "$T/pmtiles" && rm "$T/pmtiles.zip"
    fi
    if [ ! -x "$T/osmenv/Library/bin/osmium.exe" ]; then
      # micromamba 2.x crashes on some Windows hosts; 1.5.10 is reliable.
      curl -fsSL -o "$T/mm1.exe" https://github.com/mamba-org/micromamba-releases/releases/download/1.5.10-0/micromamba-win-64
      MAMBA_ROOT_PREFIX="$(cygpath -w "$T/mamba")" "$T/mm1.exe" create -y -p "$(cygpath -w "$T/osmenv")" \
        -c conda-forge osmium-tool pyosmium python=3.12
    fi
    if [ ! -x "$T/pgsql/bin/postgres.exe" ]; then
      curl -fsSL -o "$T/pg.zip" https://get.enterprisedb.com/postgresql/postgresql-18.4-1-windows-x64-binaries.zip
      unzip -q -o "$T/pg.zip" -d "$T" && rm -rf "$T/pg.zip" "$T/pgsql/pgAdmin 4" "$T/pgsql/StackBuilder"
      curl -fsSL -o "$T/postgis.zip" https://download.osgeo.org/postgis/windows/pg18/postgis-bundle-pg18-3.6.2x64.zip
      unzip -q -o "$T/postgis.zip" -d "$T/pgis_tmp"
      # -n: never overwrite PostgreSQL's own DLLs with the bundle's copies.
      cp -rn "$T"/pgis_tmp/postgis-bundle-*/* "$T/pgsql/" && rm -rf "$T/pgis_tmp" "$T/postgis.zip"
      # PostgreSQL 18 needs a newer MSVC runtime than older Windows installs ship.
      for f in concrt140 msvcp140 msvcp140_1 msvcp140_2 msvcp140_atomic_wait msvcp140_codecvt_ids vcruntime140 vcruntime140_1; do
        cp -f "$T/osmenv/$f.dll" "$T/pgsql/bin/" 2>/dev/null || true
      done
    fi
    if [ -z "$(ls "$T/redis" 2>/dev/null)" ]; then
      curl -fsSL -o "$T/redis.zip" https://github.com/redis-windows/redis-windows/releases/download/8.10.2/Redis-8.10.2-Windows-x64-msys2.zip
      mkdir -p "$T/redis" && unzip -q -o "$T/redis.zip" -d "$T/redis" && rm "$T/redis.zip"
    fi
    ;;
  Linux)
    if ! command -v osmium >/dev/null && command -v apt-get >/dev/null; then
      sudo apt-get install -y osmium-tool python3-pyosmium || true
    fi
    if ! command -v pmtiles >/dev/null && [ ! -x "$T/pmtiles/pmtiles" ]; then
      mkdir -p "$T/pmtiles"
      curl -fsSL https://github.com/protomaps/go-pmtiles/releases/download/v1.31.2/go-pmtiles_1.31.2_Linux_x86_64.tar.gz | tar xz -C "$T/pmtiles"
    fi
    ;;
  Darwin)
    command -v osmium >/dev/null || brew install osmium-tool pyosmium || true
    command -v pmtiles >/dev/null || brew install pmtiles || true
    ;;
esac
echo "tools ready"
