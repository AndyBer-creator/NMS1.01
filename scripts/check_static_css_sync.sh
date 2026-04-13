#!/usr/bin/env bash
# Собирает Tailwind и падает, если static/css/app.css отличается от закоммиченного.
set -euo pipefail
cd "$(dirname "$0")/.."
if ! command -v npm >/dev/null 2>&1; then
  echo "check_static_css_sync: npm not found; install Node.js or rely on CI job static-css-sync" >&2
  exit 1
fi
npm ci
npm run build:css
if ! git diff --exit-code -- static/css/app.css; then
  echo "check_static_css_sync: static/css/app.css out of sync. Run: make static-css && git add static/css/app.css" >&2
  exit 1
fi
echo "check_static_css_sync: OK (static/css/app.css matches build)"
