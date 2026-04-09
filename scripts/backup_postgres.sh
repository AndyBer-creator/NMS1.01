#!/usr/bin/env bash
set -euo pipefail

# NMS1 Postgres backup (docker compose)
# - Creates pg_dump custom-format archive (.dump)
# - Writes sha256 checksum
# - Deletes old backups by retention days

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BACKUP_DIR="${BACKUP_DIR:-$ROOT_DIR/backups/postgres}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
DB_NAME="${POSTGRES_DB:-NMS}"
DB_USER="${POSTGRES_USER:-nms-user}"

timestamp="$(date +%Y-%m-%dT%H-%M-%S)"
mkdir -p "$BACKUP_DIR"
out_file="$BACKUP_DIR/${DB_NAME}_${timestamp}.dump"

echo "[backup] creating: $out_file"
docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  pg_dump -U "$DB_USER" -d "$DB_NAME" -Fc > "$out_file"

sha256sum "$out_file" > "${out_file}.sha256"
echo "[backup] checksum: ${out_file}.sha256"

if [[ "$RETENTION_DAYS" =~ ^[0-9]+$ ]]; then
  find "$BACKUP_DIR" -type f \( -name '*.dump' -o -name '*.sha256' \) -mtime "+$RETENTION_DAYS" -delete
  echo "[backup] retention: deleted files older than ${RETENTION_DAYS} days"
else
  echo "[backup] WARN: BACKUP_RETENTION_DAYS is not numeric, retention skipped"
fi

echo "[backup] done"

