#!/bin/sh
# Nightly pg_dump with 14-day retention (SPEC §16). Uploads to object storage when
# BACKUP_S3_URL is set and the aws CLI is available in the image.
set -eu
while true; do
  now=$(date -u +%H%M)
  if [ "$now" = "2030" ]; then   # 02:00 IST
    f="/backups/pagdandi-$(date -u +%Y%m%d).dump"
    pg_dump -h postgres -U pagdandi -Fc pagdandi > "$f.tmp" && mv "$f.tmp" "$f"
    find /backups -name 'pagdandi-*.dump' -mtime +14 -delete
    if [ -n "${BACKUP_S3_URL:-}" ] && command -v aws >/dev/null 2>&1; then
      aws s3 cp "$f" "$BACKUP_S3_URL/" || echo "backup upload failed"
    fi
    sleep 120
  fi
  sleep 30
done
