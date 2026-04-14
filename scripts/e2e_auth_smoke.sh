#!/usr/bin/env bash
# e2e smoke с включенной авторизацией: login, cookie-сессия, доступ к защищенным роутам.
set -euo pipefail

BASE="${BASE_URL:-http://127.0.0.1:8080}"
ADMIN_USER="${NMS_ADMIN_USER:-}"
ADMIN_PASS="${NMS_ADMIN_PASS:-}"
VIEWER_USER="${NMS_VIEWER_USER:-}"
VIEWER_PASS="${NMS_VIEWER_PASS:-}"

for v in ADMIN_USER ADMIN_PASS VIEWER_USER VIEWER_PASS; do
  [[ -n "${!v:-}" ]] || { echo "e2e-auth: $v is required" >&2; exit 1; }
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
admin_jar="$tmp_dir/admin.cookies"
viewer_jar="$tmp_dir/viewer.cookies"

login() {
  local user="$1" pass="$2" jar="$3"
  local code
  code="$(
    curl -sS -o /dev/null -w "%{http_code}" \
      -c "$jar" \
      -X POST "$BASE/login" \
      -H "Content-Type: application/x-www-form-urlencoded" \
      --data-urlencode "username=$user" \
      --data-urlencode "password=$pass" \
      --data-urlencode "next=/" || true
  )"
  [[ "$code" == "302" || "$code" == "303" ]] || {
    echo "e2e-auth: login failed for $user (HTTP $code)" >&2
    exit 1
  }
}

expect_http() {
  local expected="$1" method="$2" url="$3" jar="$4"
  local code
  if [[ -n "$jar" ]]; then
    code="$(
      curl -sS -o /dev/null -w "%{http_code}" \
        -X "$method" \
        -b "$jar" \
        "$url" || true
    )"
  else
    code="$(
      curl -sS -o /dev/null -w "%{http_code}" \
        -X "$method" \
        "$url" || true
    )"
  fi
  [[ "$code" == "$expected" ]] || {
    echo "e2e-auth: $method $url expected $expected got $code" >&2
    exit 1
  }
}

# Базовая доступность публичных точек.
expect_http "200" "GET" "$BASE/health" ""
expect_http "200" "GET" "$BASE/ready" ""

# Логин и проверка защищенных роутов.
login "$ADMIN_USER" "$ADMIN_PASS" "$admin_jar"
login "$VIEWER_USER" "$VIEWER_PASS" "$viewer_jar"

expect_http "200" "GET" "$BASE/devices" "$admin_jar"
expect_http "200" "GET" "$BASE/devices" "$viewer_jar"

# Admin-only UI endpoint должен быть закрыт для viewer.
expect_http "403" "GET" "$BASE/devices/1/edit" "$viewer_jar"

# Проверка, что без auth защищенный маршрут редиректит на login.
code_anon="$(
  curl -sS -o /dev/null -w "%{http_code}" \
    "$BASE/devices" || true
)"
if [[ "$code_anon" != "302" && "$code_anon" != "303" && "$code_anon" != "401" ]]; then
  echo "e2e-auth: anonymous /devices expected redirect/unauthorized, got $code_anon" >&2
  exit 1
fi

echo "e2e-auth: OK (BASE_URL=$BASE)"
