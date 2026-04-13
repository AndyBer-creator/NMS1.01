#!/usr/bin/env bash
set -euo pipefail

# RBAC regression smoke for admin/viewer permissions.
# Required env:
#   NMS_ADMIN_USER, NMS_ADMIN_PASS, NMS_VIEWER_USER, NMS_VIEWER_PASS
# Optional:
#   BASE_URL=http://localhost:8080

BASE_URL="${BASE_URL:-http://localhost:8080}"

for v in NMS_ADMIN_USER NMS_ADMIN_PASS NMS_VIEWER_USER NMS_VIEWER_PASS; do
  [[ -n "${!v:-}" ]] || { echo "[rbac][FAIL] $v is required"; exit 1; }
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fail() { echo "[rbac][FAIL] $*" >&2; exit 1; }

login() {
  local user="$1" pass="$2" jar="$3"
  local code
  code="$(
    curl -sS -o /dev/null -w "%{http_code}" \
      -c "$jar" \
      -X POST "$BASE_URL/login" \
      -H "Content-Type: application/x-www-form-urlencoded" \
      --data-urlencode "username=$user" \
      --data-urlencode "password=$pass" \
      --data-urlencode "next=/" || true
  )"
  [[ "$code" == "302" || "$code" == "303" ]] || fail "Login failed for $user: HTTP $code"
}

ensure_csrf_cookie() {
  local jar="$1"
  curl -sS -o /dev/null -b "$jar" -c "$jar" "$BASE_URL/" >/dev/null || true
}

csrf_from_jar() {
  local jar="$1"
  awk '$6=="nms_csrf"{print $7}' "$jar" | tail -n1
}

expect_status() {
  local expected="$1" method="$2" url="$3" jar="$4" csrf="$5" body="$6" ctype="$7"
  local code
  if [[ -n "$body" ]]; then
    code="$(
      curl -sS -o /dev/null -w "%{http_code}" \
        -X "$method" "$url" \
        -b "$jar" \
        -H "X-CSRF-Token: $csrf" \
        -H "Content-Type: $ctype" \
        --data "$body" || true
    )"
  else
    code="$(
      curl -sS -o /dev/null -w "%{http_code}" \
        -X "$method" "$url" \
        -b "$jar" \
        -H "X-CSRF-Token: $csrf" || true
    )"
  fi
  [[ "$code" == "$expected" ]] || fail "$method $url expected $expected, got $code"
}

admin_jar="$tmp_dir/admin.cookies"
viewer_jar="$tmp_dir/viewer.cookies"

echo "[rbac] login admin"
login "$NMS_ADMIN_USER" "$NMS_ADMIN_PASS" "$admin_jar"
ensure_csrf_cookie "$admin_jar"
admin_csrf="$(csrf_from_jar "$admin_jar")"
[[ -n "$admin_csrf" ]] || fail "admin csrf cookie missing"

echo "[rbac] login viewer"
login "$NMS_VIEWER_USER" "$NMS_VIEWER_PASS" "$viewer_jar"
ensure_csrf_cookie "$viewer_jar"
viewer_csrf="$(csrf_from_jar "$viewer_jar")"
[[ -n "$viewer_csrf" ]] || fail "viewer csrf cookie missing"

echo "[rbac] viewer must be forbidden on admin routes"
expect_status "403" "POST" "$BASE_URL/settings/worker-poll-interval" "$viewer_jar" "$viewer_csrf" "interval_sec=60" "application/x-www-form-urlencoded"
expect_status "403" "POST" "$BASE_URL/discovery/scan" "$viewer_jar" "$viewer_csrf" "{\"cidr\":\"192.0.2.0/24\"}" "application/json"
expect_status "403" "POST" "$BASE_URL/devices" "$viewer_jar" "$viewer_csrf" "ip=192.0.2.10&name=x&community=public&snmp_version=v2c" "application/x-www-form-urlencoded"
# Редактирование строки устройства в UI — только admin; id произвольный (403 до поиска в БД).
expect_status "403" "GET" "$BASE_URL/devices/1/edit" "$viewer_jar" "$viewer_csrf" "" ""

echo "[rbac] admin must be allowed to reach same handlers"
# Expected non-403: we send minimal/invalid payloads, so 200/400 are both acceptable.
code_admin_settings="$(
  curl -sS -o /dev/null -w "%{http_code}" \
    -X POST "$BASE_URL/settings/worker-poll-interval" \
    -b "$admin_jar" \
    -H "X-CSRF-Token: $admin_csrf" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data "interval_sec=60" || true
)"
[[ "$code_admin_settings" != "403" ]] || fail "admin unexpectedly forbidden on /settings/worker-poll-interval"

code_admin_scan="$(
  curl -sS -o /dev/null -w "%{http_code}" \
    -X POST "$BASE_URL/discovery/scan" \
    -b "$admin_jar" \
    -H "X-CSRF-Token: $admin_csrf" \
    -H "Content-Type: application/json" \
    --data '{"cidr":"bad-cidr"}' || true
)"
[[ "$code_admin_scan" != "403" ]] || fail "admin unexpectedly forbidden on /discovery/scan"

echo "[rbac][OK] viewer blocked, admin allowed"

