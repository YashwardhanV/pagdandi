#!/usr/bin/env bash
# make deploy: production deploy to one VM over SSH. Skips cleanly when DEPLOY_* are unset.
set -euo pipefail
. "$(dirname "$0")/env.sh"
if [ -z "${DEPLOY_HOST:-}" ] || [ -z "${DEPLOY_USER:-}" ] || [ -z "${DEPLOY_DOMAIN:-}" ]; then
  echo "deploy skipped: set DEPLOY_HOST, DEPLOY_USER and DEPLOY_DOMAIN in .env (see docs/OPERATIONS.md)"
  exit 0
fi
(cd "$ROOT/apps/web" && npm ci && npm run build)
rsync -az --delete --exclude data/build --exclude node_modules --exclude .git --exclude .env "$ROOT/" "$DEPLOY_USER@$DEPLOY_HOST:/opt/pagdandi/"
ssh "$DEPLOY_USER@$DEPLOY_HOST" "cd /opt/pagdandi && DOMAIN=$DEPLOY_DOMAIN docker compose -f infra/docker-compose.yml --profile prod up -d --build"
echo "deployed to https://$DEPLOY_DOMAIN"
