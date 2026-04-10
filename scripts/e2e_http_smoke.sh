#!/usr/bin/env bash
# Лёгкий e2e по HTTP: health + metrics (без авторизации).
set -euo pipefail
BASE="${BASE_URL:-http://127.0.0.1:8080}"

curl -sfS "${BASE%/}/health" | grep -qx OK || {
  echo "e2e: /health failed" >&2
  exit 1
}

curl -sfS "${BASE%/}/metrics" | grep -qE '^# HELP|nms_' || {
  echo "e2e: /metrics unexpected body" >&2
  exit 1
}

echo "e2e: OK (BASE_URL=$BASE)"
