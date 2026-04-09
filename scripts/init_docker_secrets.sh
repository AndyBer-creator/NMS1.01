#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
secrets_dir="${root_dir}/.secrets"
mkdir -p "${secrets_dir}"
chmod 700 "${secrets_dir}"

write_secret() {
  local name="$1"
  local value="$2"
  if [[ -n "${value}" ]]; then
    printf '%s\n' "${value}" > "${secrets_dir}/${name}"
    chmod 600 "${secrets_dir}/${name}"
  fi
}

if [[ -f "${root_dir}/.env" ]]; then
  # shellcheck disable=SC1091
  source "${root_dir}/.env"
fi

write_secret "db_dsn" "${DB_DSN:-}"
write_secret "nms_admin_user" "${NMS_ADMIN_USER:-}"
write_secret "nms_admin_pass" "${NMS_ADMIN_PASS:-}"
write_secret "nms_viewer_user" "${NMS_VIEWER_USER:-}"
write_secret "nms_viewer_pass" "${NMS_VIEWER_PASS:-}"
write_secret "nms_session_secret" "${NMS_SESSION_SECRET:-}"
write_secret "telegram_token" "${TELEGRAM_TOKEN:-}"
write_secret "telegram_chat_id" "${TELEGRAM_CHAT_ID:-}"
write_secret "smtp_user" "${SMTP_USER:-}"
write_secret "smtp_pass" "${SMTP_PASS:-}"
write_secret "smtp_from" "${SMTP_FROM:-}"

echo "Secrets written to ${secrets_dir}"
echo "Run with overlay: docker compose -f docker-compose.yml -f docker-compose.secrets.yml up -d"
