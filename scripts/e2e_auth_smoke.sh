#!/usr/bin/env bash
# e2e smoke с включенной авторизацией:
# login -> CRUD device -> RBAC deny -> logout.
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

csrf_from_jar() {
  local jar="$1"
  awk '$0 !~ /^#/ && $6 == "nms_csrf" { token=$7 } END { if (token != "") print token }' "$jar"
}

expect_http_csrf() {
  local expected="$1" method="$2" url="$3" jar="$4"
  local csrf
  csrf="$(csrf_from_jar "$jar")"
  [[ -n "$csrf" ]] || {
    echo "e2e-auth: missing nms_csrf cookie in $jar" >&2
    exit 1
  }
  local code
  code="$(
    curl -sS -o /dev/null -w "%{http_code}" \
      -X "$method" \
      -b "$jar" \
      -H "X-CSRF-Token: $csrf" \
      "$url" || true
  )"
  [[ "$code" == "$expected" ]] || {
    echo "e2e-auth: $method $url expected $expected got $code (csrf)" >&2
    exit 1
  }
}

# Возвращает JSON body и проверяет ожидаемый HTTP-код для create.
create_device_json() {
  local expected="$1" jar="$2" ip="$3" name="$4" community="$5"
  local csrf
  csrf="$(csrf_from_jar "$jar")"
  [[ -n "$csrf" ]] || {
    echo "e2e-auth: missing nms_csrf cookie for create" >&2
    exit 1
  }
  local body_file code
  body_file="$(mktemp)"
  code="$(
    curl -sS -o "$body_file" -w "%{http_code}" \
      -X POST "$BASE/devices" \
      -b "$jar" \
      -H "X-CSRF-Token: $csrf" \
      -H "Content-Type: application/json" \
      -d "{\"ip\":\"$ip\",\"name\":\"$name\",\"community\":\"$community\"}" || true
  )"
  [[ "$code" == "$expected" ]] || {
    echo "e2e-auth: POST /devices expected $expected got $code body=$(cat "$body_file")" >&2
    rm -f "$body_file"
    exit 1
  }
  cat "$body_file"
  rm -f "$body_file"
}

# Базовая доступность публичных точек.
expect_http "200" "GET" "$BASE/health" ""
expect_http "200" "GET" "$BASE/ready" ""

# Логин и проверка защищенных роутов.
login "$ADMIN_USER" "$ADMIN_PASS" "$admin_jar"
login "$VIEWER_USER" "$VIEWER_PASS" "$viewer_jar"

expect_http "200" "GET" "$BASE/devices" "$admin_jar"
expect_http "200" "GET" "$BASE/devices" "$viewer_jar"
expect_http "200" "GET" "$BASE/" "$admin_jar"
expect_http "200" "GET" "$BASE/" "$viewer_jar"

# Admin-only UI endpoint должен быть закрыт для viewer.
expect_http "403" "GET" "$BASE/devices/1/edit" "$viewer_jar"

# CRUD device для admin.
device_ip="198.51.100.201"
device_name="e2e-auth-smoke-device"
create_body="$(create_device_json "200" "$admin_jar" "$device_ip" "$device_name" "public")"
device_id="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("id",""))' <<<"$create_body")"
[[ -n "$device_id" && "$device_id" != "0" ]] || {
  echo "e2e-auth: failed to parse created device id from body: $create_body" >&2
  exit 1
}
expect_http "200" "GET" "$BASE/devices/$device_id/edit" "$admin_jar"
expect_http_csrf "200" "DELETE" "$BASE/devices/$device_id" "$admin_jar"

# RBAC deny для mutating endpoint.
create_device_json "403" "$viewer_jar" "198.51.100.202" "viewer-must-fail" "public" >/dev/null

# Logout и проверка, что сессия погашена.
expect_http_csrf "303" "POST" "$BASE/logout" "$admin_jar"
expect_http "302" "GET" "$BASE/devices" "$admin_jar"

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
