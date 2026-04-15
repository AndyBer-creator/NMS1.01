#!/usr/bin/env bash
# Strict OpenAPI backward-compatibility check against base branch.
set -euo pipefail

SPEC_PATH="${SPEC_PATH:-internal/delivery/http/spec/openapi.yaml}"
BASE_REF="${BASE_REF:-origin/main}"
OASDIFF_IMAGE="${OASDIFF_IMAGE:-tufin/oasdiff@sha256:7728a8d4477dca2331c5df7292e32ce5d551abc4bed0c4be19645391c865406e}"

if ! command -v git >/dev/null 2>&1; then
  echo "openapi-breaking: git is required" >&2
  exit 1
fi

if [[ ! -f "$SPEC_PATH" ]]; then
  echo "openapi-breaking: current spec not found: $SPEC_PATH" >&2
  exit 1
fi

if ! git rev-parse --verify "$BASE_REF" >/dev/null 2>&1; then
  echo "openapi-breaking: base ref not found locally: $BASE_REF" >&2
  echo "openapi-breaking: hint: run 'git fetch origin main' or set BASE_REF" >&2
  exit 1
fi

if ! git cat-file -e "${BASE_REF}:${SPEC_PATH}" 2>/dev/null; then
  echo "openapi-breaking: base ref does not contain spec path: ${BASE_REF}:${SPEC_PATH}" >&2
  exit 1
fi

tmpdir="$(mktemp -d ".openapi-breaking.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT
base_spec="$tmpdir/base-openapi.yaml"

git show "${BASE_REF}:${SPEC_PATH}" >"$base_spec"

echo "openapi-breaking: comparing $SPEC_PATH against $BASE_REF"
docker run --rm \
  -v "$PWD:/work" \
  -w /work \
  "$OASDIFF_IMAGE" \
  breaking \
  --fail-on ERR \
  "$base_spec" \
  "$SPEC_PATH"

echo "openapi-breaking: OK (no breaking changes)"
