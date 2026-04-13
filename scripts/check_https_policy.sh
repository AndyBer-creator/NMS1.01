#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

if [[ "${NMS_ENFORCE_HTTPS:-}" != "true" && "${NMS_ENFORCE_HTTPS:-}" != "1" ]]; then
  echo "Skip: NMS_ENFORCE_HTTPS is not enabled in current shell."
  exit 0
fi

check_redirect() {
  local path="$1"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" "${BASE_URL}${path}" || true)"
  if [[ "${code}" != "301" && "${code}" != "302" && "${code}" != "307" && "${code}" != "308" ]]; then
    echo "FAIL: expected redirect for ${path}, got ${code}"
    exit 1
  fi
}

check_ok() {
  local path="$1"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" "${BASE_URL}${path}" || true)"
  if [[ "${code}" != "200" ]]; then
    echo "FAIL: expected 200 for ${path}, got ${code}"
    exit 1
  fi
}

check_redirect "/"
check_redirect "/login"
check_ok "/health"
check_ok "/ready"
check_ok "/metrics"
check_ok "/.well-known/security.txt"

echo "OK: HTTPS-only policy works (redirects enabled, probes bypassed)."
