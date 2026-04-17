#!/bin/sh
# Старт api/worker: bind-mount ../../logs часто приходит с хоста как root:root — nms не может
# создать nms-api.log / nms-worker.log. При запуске от root выравниваем владельца /app/logs.
set -eu
if [ "$(id -u)" -eq 0 ]; then
	mkdir -p /app/logs
	chown -R nms:nms /app/logs 2>/dev/null || true
	exec su-exec nms "$@"
fi
exec "$@"
