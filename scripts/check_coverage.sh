#!/usr/bin/env bash
# Проверка минимального покрытия по coverage.out (go tool cover -func).
set -euo pipefail
export LC_NUMERIC=C
PROFILE="${1:-coverage.out}"
MIN="${MIN_COVERAGE_PERCENT:-24}"

if [[ ! -f "$PROFILE" ]]; then
  echo "coverage: file not found: $PROFILE (run go test -coverprofile=... first)" >&2
  exit 1
fi

pct="$(go tool cover -func="$PROFILE" | awk '/^total:/ { gsub(/%/, "", $NF); print $NF + 0; exit }')"
if [[ -z "$pct" ]]; then
  echo "coverage: could not parse total from go tool cover -func" >&2
  exit 1
fi

awk -v p="$pct" -v m="$MIN" 'BEGIN {
  if (p + 0 < m + 0) {
    printf "coverage: %.1f%% is below minimum %.1f%%\n", p, m
    exit 1
  }
  printf "coverage: %.1f%% (minimum %.1f%%)\n", p, m
}'
