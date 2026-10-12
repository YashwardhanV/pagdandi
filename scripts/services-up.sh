#!/usr/bin/env bash
# Start Postgres(+PostGIS) and Redis natively. Idempotent.
set -euo pipefail
. "$(dirname "$0")/env.sh"
PGPORT="${PGPORT:-5432}"
mkdir -p "$ROOT/data/build/logs"
if [ ! -f "$PGDATA_DIR/PG_VERSION" ]; then
  echo "initialising Postgres cluster in $PGDATA_DIR"
  printf '%s' "${POSTGRES_PASSWORD:-pagdandi}" > "$ROOT/data/build/.pgpw"
  "$PGBIN/initdb" -D "$PGDATA_DIR" -U postgres --pwfile="$ROOT/data/build/.pgpw" -E UTF8 --locale=C -A scram-sha-256 >/dev/null
  rm -f "$ROOT/data/build/.pgpw"
  cat >> "$PGDATA_DIR/postgresql.conf" <<CONF
port = $PGPORT
listen_addresses = 'localhost'
shared_buffers = 256MB
work_mem = 32MB
maintenance_work_mem = 256MB
max_wal_size = 2GB
CONF
fi
if ! "$PGBIN/pg_ctl" -D "$PGDATA_DIR" status >/dev/null 2>&1; then
  "$PGBIN/pg_ctl" -D "$PGDATA_DIR" -l "$ROOT/data/build/logs/postgres.log" -w start >/dev/null
  echo "postgres started on :$PGPORT"
else
  echo "postgres already running"
fi
export PGPASSWORD="${POSTGRES_PASSWORD:-pagdandi}"
if ! "$PGBIN/psql" -h localhost -p "$PGPORT" -U postgres -tAc "select 1 from pg_roles where rolname='pagdandi'" | grep -q 1; then
  "$PGBIN/psql" -h localhost -p "$PGPORT" -U postgres -qc "create role pagdandi login superuser password '${POSTGRES_PASSWORD:-pagdandi}'"
fi
for db in pagdandi pagdandi_test; do
  if ! "$PGBIN/psql" -h localhost -p "$PGPORT" -U postgres -tAc "select 1 from pg_database where datname='$db'" | grep -q 1; then
    "$PGBIN/psql" -h localhost -p "$PGPORT" -U postgres -qc "create database $db owner pagdandi"
    echo "created database $db"
  fi
done
REDIS_PORT="${REDIS_PORT:-6379}"
if "$REDIS_DIR/redis-cli" -p "$REDIS_PORT" ping >/dev/null 2>&1; then
  echo "redis already running"
else
  (cd "$REDIS_DIR" && ./redis-server --port "$REDIS_PORT" --save "" --appendonly no --maxmemory 256mb --maxmemory-policy allkeys-lru > "$ROOT/data/build/logs/redis.log" 2>&1 &)
  for i in $(seq 1 20); do "$REDIS_DIR/redis-cli" -p "$REDIS_PORT" ping >/dev/null 2>&1 && break; sleep 0.5; done
  echo "redis started on :$REDIS_PORT"
fi
