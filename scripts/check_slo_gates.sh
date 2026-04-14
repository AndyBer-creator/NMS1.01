#!/usr/bin/env bash
set -euo pipefail

PROM_URL="${PROM_URL:-http://localhost:9090}"
SLO_API_5XX_RATIO_MAX="${SLO_API_5XX_RATIO_MAX:-0.05}"
SLO_WORKER_POLL_FAIL_10M_MAX="${SLO_WORKER_POLL_FAIL_10M_MAX:-20}"
SLO_WORKER_POLL_FAIL_RATIO_15M_MAX="${SLO_WORKER_POLL_FAIL_RATIO_15M_MAX:-0.30}"
SLO_WORKER_BACKOFF_SKIPS_15M_MAX="${SLO_WORKER_BACKOFF_SKIPS_15M_MAX:-50}"
SLO_WORKER_POLL_CYCLE_AVG_SEC_15M_MAX="${SLO_WORKER_POLL_CYCLE_AVG_SEC_15M_MAX:-180}"

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
poll_fail_ratio_15m="$(query_scalar 'sum(increase(nms_worker_poll_devices_total{status="failed"}[15m])) / clamp_min(sum(increase(nms_worker_poll_devices_total{status=~"failed|active"}[15m])), 1)')"
poll_backoff_skips_15m="$(query_scalar 'increase(nms_worker_poll_skipped_backoff_total[15m])')"
poll_cycle_avg_15m="$(query_scalar 'sum(increase(nms_worker_poll_duration_seconds_sum[15m])) / clamp_min(sum(increase(nms_worker_poll_duration_seconds_count[15m])), 1)')"

if [[ "${api_up}" == "__PROM_UNREACHABLE__" || "${worker_up}" == "__PROM_UNREACHABLE__" || "${five_xx_ratio}" == "__PROM_UNREACHABLE__" || "${poll_fail_10m}" == "__PROM_UNREACHABLE__" || "${poll_fail_ratio_15m}" == "__PROM_UNREACHABLE__" || "${poll_backoff_skips_15m}" == "__PROM_UNREACHABLE__" || "${poll_cycle_avg_15m}" == "__PROM_UNREACHABLE__" ]]; then
  echo "FAIL: Prometheus is unreachable at ${PROM_URL}"
  exit 1
fi

check_ge "API up" "${api_up}" "1"
check_ge "Worker up" "${worker_up}" "1"
check_le "API 5xx ratio (5m)" "${five_xx_ratio}" "${SLO_API_5XX_RATIO_MAX}"
check_le "Worker poll failures increase (10m)" "${poll_fail_10m}" "${SLO_WORKER_POLL_FAIL_10M_MAX}"
check_le "Worker poll failure ratio (15m)" "${poll_fail_ratio_15m}" "${SLO_WORKER_POLL_FAIL_RATIO_15M_MAX}"
check_le "Worker backoff skips increase (15m)" "${poll_backoff_skips_15m}" "${SLO_WORKER_BACKOFF_SKIPS_15M_MAX}"
check_le "Worker avg poll cycle duration (15m, sec)" "${poll_cycle_avg_15m}" "${SLO_WORKER_POLL_CYCLE_AVG_SEC_15M_MAX}"

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "All SLO gates passed."
