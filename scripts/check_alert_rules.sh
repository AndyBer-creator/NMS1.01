#!/usr/bin/env bash
set -euo pipefail

RULES_FILE="${1:-alerts/nms-alerts.yml}"
RULES_TEST_FILE="${2:-alerts/nms-alerts.test.yml}"
PROM_IMAGE="${PROM_IMAGE:-prom/prometheus:v2.55.1}"

if [[ ! -f "$RULES_FILE" ]]; then
  echo "alert-rules: file not found: $RULES_FILE" >&2
  exit 1
fi

run_promtool() {
  if command -v promtool >/dev/null 2>&1; then
    promtool "$@"
    return
  fi
  if ! command -v docker >/dev/null 2>&1; then
    echo "alert-rules: neither promtool nor docker found in PATH" >&2
    exit 1
  fi
  docker run --rm \
    --entrypoint promtool \
    -v "$PWD:/work" \
    -w /work \
    "$PROM_IMAGE" \
    "$@"
}

run_promtool check rules "$RULES_FILE"

if [[ -f "$RULES_TEST_FILE" ]]; then
  run_promtool test rules "$RULES_TEST_FILE"
  echo "alert-rules: unit tests OK ($RULES_TEST_FILE)"
fi

echo "alert-rules: OK ($RULES_FILE)"
