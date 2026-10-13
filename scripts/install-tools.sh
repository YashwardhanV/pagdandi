#!/usr/bin/env bash
# Install the data tools into data/build/tools (idempotent, no admin rights).
# Pagdandi v2 needs only: Java 17+ (GraphHopper), Go, Node, osmium and pmtiles.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$ROOT/data/build/tools"
mkdir -p "$T" "$ROOT/data/build/bin"

GH_VER=11.1
if [ ! -f "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar" ]; then
  echo "downloading GraphHopper $GH_VER"
  curl -fsSL -o "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar.part" \
    "https://github.com/graphhopper/graphhopper/releases/download/$GH_VER/graphhopper-web-$GH_VER.jar"
  mv "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar.part" "$ROOT/data/build/bin/graphhopper-web-$GH_VER.jar"
fi
command -v java >/dev/null || echo "WARNING: install Java 17+ (e.g. Temurin) for GraphHopper"
command -v go >/dev/null || echo "WARNING: install Go 1.24+"
command -v node >/dev/null || echo "WARNING: install Node 20+"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    if [ ! -x "$T/pmtiles/pmtiles.exe" ]; then
      echo "downloading pmtiles"
      curl -fsSL -o "$T/pmtiles.zip" https://github.com/protomaps/go-pmtiles/releases/download/v1.31.2/go-pmtiles_1.31.2_Windows_x86_64.zip
      mkdir -p "$T/pmtiles" && unzip -q -o "$T/pmtiles.zip" -d "$T/pmtiles" && rm "$T/pmtiles.zip"
    fi
    if [ ! -x "$T/osmenv/Library/bin/osmium.exe" ]; then
      echo "installing osmium (conda-forge, portable)"
      # micromamba 2.x crashes on some Windows hosts; 1.5.10 is reliable.
      curl -fsSL -o "$T/mm1.exe" https://github.com/mamba-org/micromamba-releases/releases/download/1.5.10-0/micromamba-win-64
      MAMBA_ROOT_PREFIX="$(cygpath -w "$T/mamba")" "$T/mm1.exe" create -y -p "$(cygpath -w "$T/osmenv")" \
        -c conda-forge osmium-tool
    fi
    ;;
  Linux)
    if ! command -v osmium >/dev/null && command -v apt-get >/dev/null; then
      sudo apt-get install -y osmium-tool || true
    fi
    if ! command -v pmtiles >/dev/null && [ ! -x "$T/pmtiles/pmtiles" ]; then
      mkdir -p "$T/pmtiles"
      curl -fsSL https://github.com/protomaps/go-pmtiles/releases/download/v1.31.2/go-pmtiles_1.31.2_Linux_x86_64.tar.gz | tar xz -C "$T/pmtiles"
    fi
    ;;
  Darwin)
    command -v osmium >/dev/null || brew install osmium-tool || true
    command -v pmtiles >/dev/null || brew install pmtiles || true
    ;;
esac
echo "tools ready"
