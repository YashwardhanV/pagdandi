#!/usr/bin/env bash
# make bootstrap: tools, .env and web dependencies. Then: make data && make dev
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { cp .env.example .env; echo "created .env"; }
./scripts/install-tools.sh
(cd apps/web && npm install --no-audit --no-fund)
(cd services/api && go mod download)
echo "bootstrap complete. Next: make data && make dev"
