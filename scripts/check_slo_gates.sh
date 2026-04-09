#!/usr/bin/env bash
set -euo pipefail

PROM_URL="${PROM_URL:-http://localhost:9090}"

query_scalar() {
  local expr="$1"
  local encoded
  encoded="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$expr")"
  local url="${PROM_URL}/api/v1/query?query=${encoded}"
  local body
  if ! body="$(curl -fsS "$url" 2>/dev/null)"; then
    echo "__PROM_UNREACHABLE__"
    return 0
  fi
  python3 -c '
import json,sys
obj=json.loads(sys.stdin.read())
if obj.get("status")!="success":
    print("nan")
    raise SystemExit(0)
res=obj.get("data",{}).get("result",[])
if not res:
    print("0")
    raise SystemExit(0)
val=res[0].get("value",[None,"0"])[1]
print(val)
' <<< "$body"
}

fail=0

check_le() {
  local name="$1" actual="$2" limit="$3"
  if ! python3 -c 'import sys; a=float(sys.argv[1]); l=float(sys.argv[2]); raise SystemExit(0 if a<=l else 1)' "$actual" "$limit"; then
    echo "FAIL: ${name}: ${actual} > ${limit}"
    fail=1
  else
    echo "OK:   ${name}: ${actual} <= ${limit}"
  fi
}

check_ge() {
  local name="$1" actual="$2" limit="$3"
  if ! python3 -c 'import sys; a=float(sys.argv[1]); l=float(sys.argv[2]); raise SystemExit(0 if a>=l else 1)' "$actual" "$limit"; then
    echo "FAIL: ${name}: ${actual} < ${limit}"
    fail=1
  else
    echo "OK:   ${name}: ${actual} >= ${limit}"
  fi
}

echo "Checking SLO gates via Prometheus: ${PROM_URL}"

api_up="$(query_scalar 'max(up{job="nms-api"})')"
worker_up="$(query_scalar 'max(up{job="nms-worker"})')"
five_xx_ratio="$(query_scalar 'sum(rate(nms_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(nms_requests_total[5m])), 1)')"
poll_fail_10m="$(query_scalar 'increase(nms_worker_poll_devices_total{status="failed"}[10m])')"

if [[ "${api_up}" == "__PROM_UNREACHABLE__" || "${worker_up}" == "__PROM_UNREACHABLE__" || "${five_xx_ratio}" == "__PROM_UNREACHABLE__" || "${poll_fail_10m}" == "__PROM_UNREACHABLE__" ]]; then
  echo "FAIL: Prometheus is unreachable at ${PROM_URL}"
  exit 1
fi

check_ge "API up" "${api_up}" "1"
check_ge "Worker up" "${worker_up}" "1"
check_le "API 5xx ratio (5m)" "${five_xx_ratio}" "0.05"
check_le "Worker poll failures increase (10m)" "${poll_fail_10m}" "20"

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "All SLO gates passed."
