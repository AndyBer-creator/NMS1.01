#!/usr/bin/env bash
set -euo pipefail

# NMS1 Postgres restore (docker compose)
# Usage:
#   RESTORE_CONFIRM_DROP=YES scripts/restore_postgres.sh /absolute/or/relative/path/to/file.dump <target_db>
#
# Notes:
# - target_db is mandatory and must be passed explicitly
# - drops/recreates target DB before restore
# - set RESTORE_CONFIRM_DROP=YES to acknowledge destructive drop/create
# - for restore-drill set RESTORE_DRILL_LOG=/path/to/log.tsv

if [[ $# -lt 2 ]]; then
  echo "Usage: RESTORE_CONFIRM_DROP=YES $0 <backup.dump> <target_db>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

dump_file="$1"
if [[ ! -f "$dump_file" ]]; then
  echo "Backup file not found: $dump_file" >&2
  exit 1
fi

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
DB_USER="${POSTGRES_USER:-nms-user}"
TARGET_DB="$2"
RESTORE_DRILL_LOG="${RESTORE_DRILL_LOG:-}"
RESTORE_CONFIRM_DROP="${RESTORE_CONFIRM_DROP:-}"
PRIMARY_DB="${POSTGRES_DB:-NMS}"
restore_started_epoch="$(date +%s)"

# Accept only simple PostgreSQL identifiers to prevent SQL injection.
# (unquoted identifier: starts with letter/_ and then letter/digit/_)
if [[ ! "$TARGET_DB" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "Invalid target_db: '$TARGET_DB'. Allowed pattern: ^[A-Za-z_][A-Za-z0-9_]*$" >&2
  exit 1
fi

if [[ "$RESTORE_CONFIRM_DROP" != "YES" ]]; then
  echo "Refusing destructive restore without RESTORE_CONFIRM_DROP=YES" >&2
  exit 1
fi

if [[ "$TARGET_DB" == "$PRIMARY_DB" ]]; then
  echo "Refusing restore into primary/default DB '$TARGET_DB'. Use a dedicated restore target database." >&2
  exit 1
fi

echo "[restore] source: $dump_file"
echo "[restore] target database: $TARGET_DB"
echo "[restore] primary/default database: $PRIMARY_DB"

if [[ -f "${dump_file}.sha256" ]]; then
  echo "[restore] verifying checksum"
  (cd "$(dirname "$dump_file")" && sha256sum -c "$(basename "${dump_file}.sha256")")
else
  echo "[restore] WARN: checksum file not found (${dump_file}.sha256), continue without verification"
fi

if [[ -f "${dump_file}.meta" ]]; then
  echo "[restore] metadata:"
  sed 's/^/[restore]   /' "${dump_file}.meta"
else
  echo "[restore] WARN: metadata file not found (${dump_file}.meta)"
fi

echo "[restore] terminating active connections"
docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${TARGET_DB}' AND pid <> pg_backend_pid();"

echo "[restore] dropping and creating database"
docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS \"${TARGET_DB}\";"
docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "CREATE DATABASE \"${TARGET_DB}\";"

echo "[restore] restoring data"
cat "$dump_file" | docker compose -f "$COMPOSE_FILE" exec -T "$POSTGRES_CONTAINER" \
  pg_restore -U "$DB_USER" -d "$TARGET_DB" --clean --if-exists --no-owner --no-privileges

restore_finished_epoch="$(date +%s)"
restore_duration_sec="$((restore_finished_epoch - restore_started_epoch))"
echo "[restore] duration_sec: $restore_duration_sec"

if [[ -n "$RESTORE_DRILL_LOG" ]]; then
  mkdir -p "$(dirname "$RESTORE_DRILL_LOG")"
  printf "%s\t%s\t%s\t%s\t%s\n" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "$dump_file" \
    "$TARGET_DB" \
    "$restore_duration_sec" \
    "ok" >> "$RESTORE_DRILL_LOG"
  echo "[restore] drill log updated: $RESTORE_DRILL_LOG"
fi

echo "[restore] done"

