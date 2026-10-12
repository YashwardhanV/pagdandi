#!/usr/bin/env bash
# make tunnel: HTTPS quick tunnel to the web dev server, so GPS works on a phone.
set -euo pipefail
. "$(dirname "$0")/env.sh"
CF="$(command -v cloudflared || true)"
if [ -z "$CF" ]; then
  mkdir -p "$TOOLS/cloudflared"
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
      CF="$TOOLS/cloudflared/cloudflared.exe"
      [ -x "$CF" ] || curl -fsSL -o "$CF" https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe ;;
    Darwin)
      brew install cloudflared; CF=cloudflared ;;
    *)
      CF="$TOOLS/cloudflared/cloudflared"
      [ -x "$CF" ] || { curl -fsSL -o "$CF" https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64; chmod +x "$CF"; } ;;
  esac
fi
echo "Open the printed https://…trycloudflare.com URL on your phone. The web dev server proxies /v1 to the API."
exec "$CF" tunnel --url http://localhost:5173
