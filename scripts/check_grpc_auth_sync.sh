#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE_CMD=(docker compose --env-file .env -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.secrets.yml)

fail() {
  echo "[grpc-auth-sync][FAIL] $*" >&2
  exit 1
}

echo "[grpc-auth-sync] checking running services"
api_state="$("${COMPOSE_CMD[@]}" ps -q api || true)"
trap_state="$("${COMPOSE_CMD[@]}" ps -q trap-receiver || true)"
[[ -n "$api_state" ]] || fail "api service is not running"
[[ -n "$trap_state" ]] || fail "trap-receiver service is not running"

echo "[grpc-auth-sync] checking DB-backed token setting"
db_has_token="$("${COMPOSE_CMD[@]}" exec -T postgres sh -lc \
  "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"SELECT CASE WHEN EXISTS (SELECT 1 FROM nms_settings WHERE key='grpc_auth_token_secret' AND (COALESCE(value,'') <> '' OR COALESCE(value_enc,'') <> '')) THEN '1' ELSE '0' END\"" \
  | tr -d '[:space:]' || true)"

if [[ "$db_has_token" == "1" ]]; then
  echo "[grpc-auth-sync][OK] DB token is configured (api + trap-receiver use the same DB key grpc_auth_token_secret)"
  exit 0
fi

echo "[grpc-auth-sync] DB token is empty, checking file/env fallback"
token_file=".secrets/nms_grpc_auth_token"
[[ -f "$token_file" ]] || fail "DB token is empty and $token_file not found"
token="$(tr -d '\r\n' < "$token_file")"
[[ -n "${token// }" ]] || fail "DB token is empty and $token_file is blank"

echo "[grpc-auth-sync][OK] fallback token file is present (.secrets/nms_grpc_auth_token)"
