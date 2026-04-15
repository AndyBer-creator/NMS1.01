#!/usr/bin/env bash
set -euo pipefail

# NMS1 Postgres backup (docker compose)
# - Creates pg_dump custom-format archive (.dump)
# - Writes sha256 checksum
# - Deletes old backups by retention days
# - Optionally pushes backup to offsite storage and immutable tier

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BACKUP_DIR="${BACKUP_DIR:-$ROOT_DIR/backups/postgres}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
DB_NAME="${POSTGRES_DB:-NMS}"
DB_USER="${POSTGRES_USER:-nms-user}"
RPO_TARGET_MINUTES="${RPO_TARGET_MINUTES:-60}"
RTO_TARGET_MINUTES="${RTO_TARGET_MINUTES:-120}"
OFFSITE_SYNC_CMD="${BACKUP_OFFSITE_SYNC_CMD:-}"
IMMUTABLE_COPY_CMD="${BACKUP_IMMUTABLE_COPY_CMD:-}"

timestamp="$(date +%Y-%m-%dT%H-%M-%S)"
mkdir -p "$BACKUP_DIR"
out_file="$BACKUP_DIR/${DB_NAME}_${timestamp}.dump"

echo "[backup] creating: $out_file"
docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  pg_dump -U "$DB_USER" -d "$DB_NAME" -Fc > "$out_file"

sha256sum "$out_file" > "${out_file}.sha256"
echo "[backup] checksum: ${out_file}.sha256"

manifest_file="${out_file}.meta"
{
  echo "backup_file=$(basename "$out_file")"
  echo "created_at_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "rpo_target_minutes=$RPO_TARGET_MINUTES"
  echo "rto_target_minutes=$RTO_TARGET_MINUTES"
  echo "compose_file=$COMPOSE_FILE"
  echo "postgres_container=$POSTGRES_CONTAINER"
  echo "database=$DB_NAME"
} > "$manifest_file"
echo "[backup] manifest: $manifest_file"

if [[ -n "$OFFSITE_SYNC_CMD" ]]; then
  echo "[backup] offsite sync start"
  env BACKUP_FILE="$out_file" BACKUP_SHA256_FILE="${out_file}.sha256" BACKUP_META_FILE="$manifest_file" \
    bash -lc "$OFFSITE_SYNC_CMD"
  echo "[backup] offsite sync done"
else
  echo "[backup] WARN: BACKUP_OFFSITE_SYNC_CMD is empty (offsite copy skipped)"
fi

if [[ -n "$IMMUTABLE_COPY_CMD" ]]; then
  echo "[backup] immutable copy start"
  env BACKUP_FILE="$out_file" BACKUP_SHA256_FILE="${out_file}.sha256" BACKUP_META_FILE="$manifest_file" \
    bash -lc "$IMMUTABLE_COPY_CMD"
  echo "[backup] immutable copy done"
else
  echo "[backup] WARN: BACKUP_IMMUTABLE_COPY_CMD is empty (immutable copy skipped)"
fi

if [[ "$RETENTION_DAYS" =~ ^[0-9]+$ ]]; then
  find "$BACKUP_DIR" -type f \( -name '*.dump' -o -name '*.sha256' -o -name '*.meta' \) -mtime "+$RETENTION_DAYS" -delete
  echo "[backup] retention: deleted files older than ${RETENTION_DAYS} days"
else
  echo "[backup] WARN: BACKUP_RETENTION_DAYS is not numeric, retention skipped"
fi

echo "[backup] done"

