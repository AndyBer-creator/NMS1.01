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

check_docker_sock_readonly() {
  local file="$1"
  local sock_lines
  sock_lines="$("$search_cmd" "${search_args[@]}" "/var/run/docker\\.sock:" "$file" || true)"
  if [[ -z "$sock_lines" ]]; then
    return
  fi
  if ! "$search_cmd" "${search_args[@]}" "/var/run/docker\\.sock:[^[:space:]]*:ro\\s*$" "$file" >/dev/null; then
    echo "ERROR: docker.sock must be mounted read-only ($file)"
    echo "$sock_lines"
    fail=1
  fi
}

for compose_file in deploy/compose/docker-compose.yml deploy/compose/docker-compose.bridge.yml; do
  check_forbidden "privileged:\\s*true" "$compose_file" "privileged containers are forbidden"
  check_forbidden "image:\\s*[^[:space:]]+:latest" "$compose_file" "latest image tags are forbidden"
  check_forbidden "^\\s*disable:\\s*true\\s*$" "$compose_file" "disabled healthcheck is forbidden"
  check_forbidden "^\\s*pid:\\s*host\\s*$" "$compose_file" "pid host mode is forbidden"
  check_forbidden "^\\s*ipc:\\s*host\\s*$" "$compose_file" "ipc host mode is forbidden"
  check_forbidden "seccomp=unconfined|apparmor=unconfined" "$compose_file" "unconfined security_opt is forbidden"
  check_docker_sock_readonly "$compose_file"
  check_forbidden "^\\s*-\\s*\"(0\\.0\\.0\\.0:)?(5432|9090|9093|3000|8080):" "$compose_file" "management ports must not be published on all interfaces"
  check_forbidden "^\\s*-\\s*(SYS_ADMIN|SYS_MODULE|DAC_READ_SEARCH|DAC_OVERRIDE|SYS_PTRACE|NET_ADMIN)\\s*$" "$compose_file" "dangerous Linux capabilities in cap_add are forbidden"
done

check_forbidden "^\\s*network_mode:\\s*host\\s*$" "deploy/compose/docker-compose.bridge.yml" "bridge compose must not use host network mode"
check_forbidden "^FROM\\s+[^[:space:]]+:latest" "Dockerfile" "latest base image tags are forbidden"

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

echo "compose security checks passed"
