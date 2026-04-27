#!/usr/bin/env sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <template> <output>" >&2
  exit 1
fi

template_path="$1"
output_path="$2"

token="${NMS_ALERT_WEBHOOK_TOKEN:-}"
token_file="${NMS_ALERT_WEBHOOK_TOKEN_FILE:-}"
rendered_token_file=""

if [ -z "$token" ] && [ -n "$token_file" ] && [ -r "$token_file" ]; then
  token="$(tr -d '\r\n' < "$token_file")"
fi

auth_block=""
if [ -n "$token" ]; then
  rendered_token_file="${TMPDIR:-/tmp}/nms-alertmanager-webhook-token"
  umask 077
  printf '%s' "$token" > "$rendered_token_file"
  trap 'rm -f "$rendered_token_file"' EXIT INT TERM
  auth_block="$(cat <<EOF
        http_config:
          authorization:
            type: Bearer
            credentials_file: $rendered_token_file
EOF
)"
fi

mkdir -p "$(dirname "$output_path")"
: > "$output_path"

while IFS= read -r line || [ -n "$line" ]; do
  if [ "$line" = "__ALERTMANAGER_WEBHOOK_AUTH_BLOCK__" ]; then
    if [ -n "$auth_block" ]; then
      printf '%s\n' "$auth_block" >> "$output_path"
    fi
    continue
  fi
  printf '%s\n' "$line" >> "$output_path"
done < "$template_path"
