#!/usr/bin/env bash
# Скачивает версионированные JS-ассеты в static/js (без CDN в рантайме).
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p static/js
curl -fsSL "https://unpkg.com/htmx.org@1.9.10/dist/htmx.min.js" -o static/js/htmx.min.js
curl -fsSL "https://unpkg.com/vis-network@9.1.9/standalone/umd/vis-network.min.js" -o static/js/vis-network.min.js
echo "OK: static/js/htmx.min.js static/js/vis-network.min.js"
