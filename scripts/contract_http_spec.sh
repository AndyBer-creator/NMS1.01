#!/usr/bin/env bash
# Лёгкая contract-проверка встроенных HTTP-спецификаций.
set -euo pipefail

BASE="${BASE_URL:-http://127.0.0.1:8080}"

fetch() {
  local path="$1"
  curl -sfS "${BASE%/}${path}"
}

assert_regex() {
  local body="$1"
  local regex="$2"
  local fail_msg="$3"
  printf '%s\n' "$body" | grep -qE "$regex" || {
    echo "$fail_msg" >&2
    exit 1
  }
}

openapi_body="$(fetch "/api/openapi.yaml")"
assert_regex "$openapi_body" '^openapi: 3\.' "contract: /api/openapi.yaml is missing OpenAPI header"
assert_regex "$openapi_body" '^paths:' "contract: /api/openapi.yaml is missing paths section"
assert_regex "$openapi_body" '/devices:' "contract: /api/openapi.yaml is missing /devices path"

security_body="$(fetch "/.well-known/security.txt")"
assert_regex "$security_body" '^Contact:' "contract: security.txt is missing Contact field"
assert_regex "$security_body" '^Canonical:' "contract: security.txt is missing Canonical field"

echo "contract: OK (BASE_URL=$BASE)"
