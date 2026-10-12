#!/usr/bin/env bash
# make bootstrap: tools, .env, Postgres+Redis, migrations, web deps (SPEC §13.2).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ ! -f .env ]; then
  cp .env.example .env
  secret="$(head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 48)"
  sed -i.bak "s|^SESSION_SECRET=.*|SESSION_SECRET=$secret|" .env && rm -f .env.bak
  echo "created .env with a fresh SESSION_SECRET"
fi

./scripts/install-tools.sh

if [ "${PAGDANDI_RUNTIME:-}" != "native" ] && docker info >/dev/null 2>&1; then
  echo "starting Postgres and Redis with Docker Compose"
  docker compose -f infra/docker-compose.yml --profile dev up -d postgres redis
  for _ in $(seq 1 60); do
    docker compose -f infra/docker-compose.yml exec -T postgres pg_isready -U pagdandi >/dev/null 2>&1 && break
    sleep 1
  done
  docker compose -f infra/docker-compose.yml exec -T postgres psql -U pagdandi -tc "select 1 from pg_database where datname='pagdandi_test'" | grep -q 1 \
    || docker compose -f infra/docker-compose.yml exec -T postgres createdb -U pagdandi pagdandi_test
else
  echo "Docker unavailable: starting native Postgres and Redis"
  ./scripts/services-up.sh
fi

. scripts/env.sh
(cd services/api && go run ./cmd/datactl migrate)
(cd apps/web && npm install --no-audit --no-fund)
echo "bootstrap complete. Next: make data REGION=indore && make dev"
