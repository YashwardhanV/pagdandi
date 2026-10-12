#!/usr/bin/env bash
# make test-gh: build the synthetic-town graph through the real pipeline, serve it,
# and run the deterministic profile and integration tests (SPEC §14).
set -euo pipefail
. "$(dirname "$0")/env.sh"
TEST_DB="${TEST_DATABASE_URL:-postgres://pagdandi:pagdandi@localhost:5432/pagdandi_test?sslmode=disable}"
mkdir -p "$ROOT/data/build/logs"
"$OSM_PY" "$ROOT/infra/tools/test_osm_overrides.py"
cd "$ROOT/services/api"
DATABASE_URL="$TEST_DB" GH_JAVA_XMX=512m go run ./cmd/datactl fixture
java -Xmx256m -jar "$GH_JAR" server "$ROOT/data/build/fixture/gh/in/config.yml" >"$ROOT/data/build/logs/gh-fixture.log" 2>&1 &
GH_PID=$!
cleanup() {
  kill "$GH_PID" 2>/dev/null || true
  if command -v taskkill >/dev/null; then taskkill //F //PID "$GH_PID" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT
for _ in $(seq 1 90); do curl -fs localhost:8999/health >/dev/null && break; sleep 1; done
TEST_DATABASE_URL="$TEST_DB" TEST_REDIS_URL="${TEST_REDIS_URL:-redis://localhost:6379/1}" \
  PAGDANDI_FIXTURE_GH_URL=http://localhost:8999 go test -count=1 -v ./internal/integration/
