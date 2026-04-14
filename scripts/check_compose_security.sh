#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail=0
search_cmd="grep"
search_args=(-nE)
if command -v rg >/dev/null 2>&1; then
  search_cmd="rg"
  search_args=(-n)
fi

check_forbidden() {
  local pattern="$1"
  local file="$2"
  local message="$3"
  if "$search_cmd" "${search_args[@]}" "$pattern" "$file" >/dev/null; then
    echo "ERROR: $message ($file)"
    "$search_cmd" "${search_args[@]}" "$pattern" "$file" || true
    fail=1
  fi
}

for compose_file in docker-compose.yml docker-compose.bridge.yml; do
  check_forbidden "privileged:\\s*true" "$compose_file" "privileged containers are forbidden"
  check_forbidden "image:\\s*[^[:space:]]+:latest" "$compose_file" "latest image tags are forbidden"
  check_forbidden "healthcheck:\\s*\\n\\s*disable:\\s*true" "$compose_file" "disabled healthcheck is forbidden"
done

check_forbidden "^FROM\\s+[^[:space:]]+:latest" "Dockerfile" "latest base image tags are forbidden"

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

echo "compose security checks passed"
