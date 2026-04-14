#!/usr/bin/env bash
set -euo pipefail

OUTPUT="${1:-sbom.spdx.json}"
IMAGE="${SYFT_IMAGE:-anchore/syft:1.20.0}"

docker run --rm \
  -v "$PWD:/src" \
  "$IMAGE" \
  "dir:/src" \
  "-o" "spdx-json=/src/${OUTPUT}"

echo "sbom: wrote ${OUTPUT}"
