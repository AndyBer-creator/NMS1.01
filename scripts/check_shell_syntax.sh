#!/usr/bin/env bash
set -euo pipefail

failed=0

for f in scripts/*.sh; do
  if [[ ! -f "$f" ]]; then
    continue
  fi
  if bash -n "$f"; then
    echo "shell-syntax: OK   $f"
  else
    echo "shell-syntax: FAIL $f" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "shell-syntax: all scripts are valid"
