#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
logs_dir="${root_dir}/logs"

if [[ ! -d "${logs_dir}" ]]; then
  echo "logs directory not found: ${logs_dir}"
  echo "Nothing to scan; pass."
  exit 0
fi

fail=0

# 1) Suspicious key names in logs.
if rg -n -i \
  -e 'postgres_password' \
  -e 'db_dsn' \
  -e 'nms_admin_pass' \
  -e 'nms_viewer_pass' \
  -e 'nms_session_secret' \
  -e 'telegram_token' \
  -e 'smtp_pass' \
  "${logs_dir}" >/dev/null 2>&1; then
  echo "FAIL: secret key names detected in logs."
  rg -n -i \
    -e 'postgres_password' \
    -e 'db_dsn' \
    -e 'nms_admin_pass' \
    -e 'nms_viewer_pass' \
    -e 'nms_session_secret' \
    -e 'telegram_token' \
    -e 'smtp_pass' \
    "${logs_dir}" || true
  fail=1
fi

# 2) Exact secret values from environment (if present) should not appear in logs.
check_env_value() {
  local key="$1"
  local val="${!key:-}"
  if [[ -n "${val}" ]]; then
    if rg -n -F -- "${val}" "${logs_dir}" >/dev/null 2>&1; then
      echo "FAIL: value of ${key} found in logs."
      fail=1
    fi
  fi
}

for key in \
  POSTGRES_PASSWORD DB_DSN NMS_ADMIN_PASS NMS_VIEWER_PASS NMS_SESSION_SECRET \
  TELEGRAM_TOKEN TELEGRAM_CHAT_ID SMTP_PASS SMTP_USER SMTP_FROM
do
  check_env_value "${key}"
done

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "OK: no secret leaks detected in logs."
