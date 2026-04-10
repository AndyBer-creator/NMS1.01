#!/usr/bin/env bash
# Лёгкая нагрузка только на read-only эндпоинты: /health и /metrics (без авторизации).
# Требования: bash с wait -n (4.3+), curl.
set -euo pipefail

BASE="${BASE_URL:-http://127.0.0.1:8080}"
BASE="${BASE%/}"
REQS="${LOAD_REQUESTS:-200}"
CONC="${LOAD_CONCURRENCY:-25}"

if ! [[ "$REQS" =~ ^[0-9]+$ ]] || ! [[ "$CONC" =~ ^[0-9]+$ ]]; then
  echo "load: LOAD_REQUESTS and LOAD_CONCURRENCY must be integers" >&2
  exit 1
fi
if (( CONC < 1 || REQS < 1 )); then
  echo "load: REQS and CONC must be >= 1" >&2
  exit 1
fi

faildir="$(mktemp -d)"
trap 'rm -rf "$faildir"' EXIT

curl_one() {
  local i="$1"
  local url
  if (( i % 2 == 0 )); then
    url="$BASE/health"
  else
    url="$BASE/metrics"
  fi
  if ! curl -sfS --max-time 15 "$url" >/dev/null; then
    touch "$faildir/fail-$i-$$-$RANDOM"
  fi
}

export -f curl_one
export BASE faildir

for ((i = 0; i < REQS; i++)); do
  while (( $(jobs -rp | wc -l) >= CONC )); do
    wait -n 2>/dev/null || true
  done
  bash -c "curl_one $i" &
done
wait

nfail="$(find "$faildir" -type f 2>/dev/null | wc -l | tr -d ' ')"
if (( nfail > 0 )); then
  echo "load: failed requests: $nfail / $REQS (BASE_URL=$BASE conc=$CONC)" >&2
  exit 1
fi

echo "load: OK $REQS requests (conc=$CONC, BASE_URL=$BASE)"
