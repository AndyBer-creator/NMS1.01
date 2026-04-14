#!/usr/bin/env bash
set -euo pipefail

failed=0

check_no_matches() {
  local pattern="$1"
  local target="$2"
  local label="$3"
  if rg -n --no-heading "$pattern" "$target" >/tmp/nms_tool_versions_check.out 2>/dev/null; then
    echo "tool-versions: FAIL ($label) unexpected floating version pattern found in $target" >&2
    while IFS= read -r line; do
      echo "  $line" >&2
    done </tmp/nms_tool_versions_check.out
    failed=1
  else
    echo "tool-versions: OK   ($label) $target"
  fi
}

# Disallow floating Go module tool versions.
check_no_matches '@latest\b' '.github/workflows' 'go tool @latest in workflows'
check_no_matches '@latest\b' 'Makefile' 'go tool @latest in Makefile'

# Disallow mutable GitHub Action refs.
check_no_matches 'uses:\s*[^[:space:]]+@(main|master)\b' '.github/workflows' 'mutable action refs'

rm -f /tmp/nms_tool_versions_check.out

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "tool-versions: all checks passed"
