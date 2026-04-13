#!/usr/bin/env bash
set -euo pipefail

# NMS1 post-deploy smoke test
# Usage:
#   ./scripts/smoke_test.sh
# Optional env:
#   BASE_URL=http://localhost:8080
#   WORKER_METRICS_URL=http://localhost:8081/metrics
#   NMS_ADMIN_USER=admin
#   NMS_ADMIN_PASS=secret

BASE_URL="${BASE_URL:-http://localhost:8080}"
WORKER_METRICS_URL="${WORKER_METRICS_URL:-http://localhost:8081/metrics}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
cookie_jar="$tmp_dir/cookies.txt"

fail() {
  echo "[smoke][FAIL] $*" >&2
  exit 1
}

expect_http_200() {
  local url="$1"
  local out="$2"
  local code
  code="$(curl -sS -o "$out" -w "%{http_code}" "$url" || true)"
  [[ "$code" == "200" ]] || fail "Expected 200 from $url, got $code"
}

expect_http_200_cookie() {
  local url="$1"
  local out="$2"
  local cookie="$3"
  local code
  code="$(curl -sS -o "$out" -w "%{http_code}" -b "$cookie" "$url" || true)"
  [[ "$code" == "200" ]] || fail "Expected 200 from $url, got $code"
}

echo "[smoke] BASE_URL=$BASE_URL"
echo "[smoke] WORKER_METRICS_URL=$WORKER_METRICS_URL"

echo "[smoke] 1/8 health endpoint"
expect_http_200 "$BASE_URL/health" "$tmp_dir/health.txt"

echo "[smoke] 2/8 readiness endpoint"
expect_http_200 "$BASE_URL/ready" "$tmp_dir/ready.txt"
grep -q '"status"' "$tmp_dir/ready.txt" || fail "ready JSON missing status"

echo "[smoke] 3/8 api metrics endpoint"
expect_http_200 "$BASE_URL/metrics" "$tmp_dir/api_metrics.txt"
grep -q "nms_requests_total" "$tmp_dir/api_metrics.txt" || fail "nms_requests_total not found in API metrics"

echo "[smoke] 4/8 login page"
expect_http_200 "$BASE_URL/login" "$tmp_dir/login_page.html"
grep -qi "form" "$tmp_dir/login_page.html" || fail "login page does not contain form"

if [[ -n "${NMS_ADMIN_USER:-}" && -n "${NMS_ADMIN_PASS:-}" ]]; then
  echo "[smoke] 5/8 login as admin"
  login_code="$(
    curl -sS -o "$tmp_dir/login_post.txt" -w "%{http_code}" \
      -c "$cookie_jar" \
      -X POST "$BASE_URL/login" \
      -H "Content-Type: application/x-www-form-urlencoded" \
      --data-urlencode "username=${NMS_ADMIN_USER}" \
      --data-urlencode "password=${NMS_ADMIN_PASS}" \
      --data-urlencode "next=/" || true
  )"
  [[ "$login_code" == "302" || "$login_code" == "303" ]] || fail "Login failed: expected 302/303, got $login_code"

  echo "[smoke] 6/8 authenticated devices list"
  devices_code="$(
    curl -sS -o "$tmp_dir/devices_page.html" -w "%{http_code}" \
      -b "$cookie_jar" \
      "$BASE_URL/devices/list" || true
  )"
  [[ "$devices_code" == "200" ]] || fail "Expected 200 from /devices/list with auth, got $devices_code"
  grep -q "Таблица устройств" "$tmp_dir/devices_page.html" || fail "Devices page content check failed"

  echo "[smoke] 7/8 availability page"
  expect_http_200_cookie "$BASE_URL/events/availability/page" "$tmp_dir/availability_page.html" "$cookie_jar"
else
  echo "[smoke] 5-7/8 skipped login-dependent checks (NMS_ADMIN_USER/PASS not set)"
fi

echo "[smoke] 8/8 worker metrics endpoint"
expect_http_200 "$WORKER_METRICS_URL" "$tmp_dir/worker_metrics.txt"
grep -q "nms_worker_poll_duration_seconds" "$tmp_dir/worker_metrics.txt" || fail "worker metrics missing nms_worker_poll_duration_seconds"

echo "[smoke][OK] all checks passed"

