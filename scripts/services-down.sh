#!/usr/bin/env bash
# Stop native Postgres and Redis.
. "$(dirname "$0")/env.sh"
"$PGBIN/pg_ctl" -D "$PGDATA_DIR" -m fast stop 2>/dev/null || true
"$REDIS_DIR/redis-cli" -p "${REDIS_PORT:-6379}" shutdown nosave 2>/dev/null || true
echo "stopped"
