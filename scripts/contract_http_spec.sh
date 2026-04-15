#!/usr/bin/env bash
# Contract-проверка встроенных HTTP-спецификаций c auth expectations.
set -euo pipefail

BASE="${BASE_URL:-http://127.0.0.1:8080}"
ADMIN_USER="${NMS_ADMIN_USER:-}"
ADMIN_PASS="${NMS_ADMIN_PASS:-}"

fetch() {
  local path="$1"
  curl -sfS "${BASE%/}${path}"
}

fetch_basic() {
  local path="$1"
  curl -sfS -u "${ADMIN_USER}:${ADMIN_PASS}" "${BASE%/}${path}"
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

if [[ -z "$ADMIN_USER" || -z "$ADMIN_PASS" ]]; then
  echo "contract: NMS_ADMIN_USER and NMS_ADMIN_PASS are required" >&2
  exit 1
fi

openapi_anon_code="$(
  curl -sS -o /dev/null -w "%{http_code}" "${BASE%/}/api/openapi.yaml" || true
)"
if [[ "$openapi_anon_code" != "302" && "$openapi_anon_code" != "303" && "$openapi_anon_code" != "401" ]]; then
  echo "contract: /api/openapi.yaml must require auth (got HTTP $openapi_anon_code for anonymous)" >&2
  exit 1
fi

openapi_body="$(fetch_basic "/api/openapi.yaml")"
assert_regex "$openapi_body" '^openapi: 3\.' "contract: /api/openapi.yaml is missing OpenAPI header"
assert_regex "$openapi_body" '^paths:' "contract: /api/openapi.yaml is missing paths section"
assert_regex "$openapi_body" '/devices:' "contract: /api/openapi.yaml is missing /devices path"
assert_regex "$openapi_body" '/incidents:' "contract: /api/openapi.yaml is missing /incidents path"
assert_regex "$openapi_body" '/incidents/\{incidentID\}:' "contract: /api/openapi.yaml is missing /incidents/{incidentID} path"
assert_regex "$openapi_body" '/incidents/bulk/status:' "contract: /api/openapi.yaml is missing /incidents/bulk/status path"
assert_regex "$openapi_body" '/trap-oid-mappings:' "contract: /api/openapi.yaml is missing /trap-oid-mappings path"
assert_regex "$openapi_body" 'IncidentBulkTransitionRequest' "contract: openapi is missing IncidentBulkTransitionRequest schema"
assert_regex "$openapi_body" 'IncidentBulkTransitionResult' "contract: openapi is missing IncidentBulkTransitionResult schema"
assert_regex "$openapi_body" 'IncidentListPage' "contract: openapi is missing IncidentListPage schema"
assert_regex "$openapi_body" 'TrapOIDMapping' "contract: openapi is missing TrapOIDMapping schema"
assert_regex "$openapi_body" 'sessionOrBasic' "contract: openapi must define auth scheme sessionOrBasic"

security_body="$(fetch "/.well-known/security.txt")"
assert_regex "$security_body" '^Contact:' "contract: security.txt is missing Contact field"
assert_regex "$security_body" '^Canonical:' "contract: security.txt is missing Canonical field"

incidents_body="$(fetch_basic "/incidents?limit=1&include_pagination=true")"
assert_regex "$incidents_body" '"items"' "contract: /incidents paginated response must include items field"

echo "contract: OK (BASE_URL=$BASE)"
