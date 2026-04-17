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
write_secret "nms_db_encryption_key" "${NMS_DB_ENCRYPTION_KEY:-}"
write_secret "nms_alert_webhook_token" "${NMS_ALERT_WEBHOOK_TOKEN:-}"
write_secret "nms_itsm_inbound_token" "${NMS_ITSM_INBOUND_TOKEN:-}"
if [[ -n "${NMS_TERMINAL_SSH_KNOWN_HOSTS:-}" ]]; then
  if [[ -f "${NMS_TERMINAL_SSH_KNOWN_HOSTS}" ]]; then
    chmod 600 "${NMS_TERMINAL_SSH_KNOWN_HOSTS}" 2>/dev/null || true
    cp "${NMS_TERMINAL_SSH_KNOWN_HOSTS}" "${secrets_dir}/nms_terminal_known_hosts"
    chmod 600 "${secrets_dir}/nms_terminal_known_hosts"
  else
    write_secret "nms_terminal_known_hosts" "${NMS_TERMINAL_SSH_KNOWN_HOSTS:-}"
  fi
fi
write_secret "telegram_token" "${TELEGRAM_TOKEN:-}"
write_secret "telegram_chat_id" "${TELEGRAM_CHAT_ID:-}"
write_secret "smtp_user" "${SMTP_USER:-}"
write_secret "smtp_pass" "${SMTP_PASS:-}"
write_secret "smtp_from" "${SMTP_FROM:-}"

echo "Secrets written to ${secrets_dir}"
echo "Run with overlay: docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.secrets.yml up -d"
