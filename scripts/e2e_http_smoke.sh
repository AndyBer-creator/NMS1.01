#!/usr/bin/env bash
# Лёгкий e2e по HTTP: health + metrics (без авторизации).
set -euo pipefail
BASE="${BASE_URL:-http://127.0.0.1:8080}"

check_body_regex() {
  local path="$1"
  local regex="$2"
  local fail_msg="$3"
  local body
  body="$(curl -sfS "${BASE%/}${path}")"
  printf '%s\n' "$body" | grep -qE "$regex" || {
    echo "$fail_msg" >&2
    exit 1
  }
}

check_body_regex "/health" '^OK$' "e2e: /health failed"
check_body_regex "/ready" '"status"' "e2e: /ready failed (expected JSON with status)"
check_body_regex "/metrics" '^# HELP|nms_' "e2e: /metrics unexpected body"

echo "e2e: OK (BASE_URL=$BASE)"
