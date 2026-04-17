# Terminal SSH Quickstart

Короткая памятка: как добавить новый свич в `known_hosts` и как за 2 команды диагностировать web-terminal (WS/SSH).

## Добавить новый свич в known_hosts

1. Считать host key свича:

```bash
mkdir -p .secrets
ssh-keyscan -T 5 -H 192.168.10.10 >> .secrets/known_hosts
```

2. Убедиться, что API использует файл:

```bash
grep -n '^NMS_TERMINAL_SSH_KNOWN_HOSTS=' .env
# ожидается: NMS_TERMINAL_SSH_KNOWN_HOSTS=/app/secrets/known_hosts
```

3. Перезапустить API:

```bash
docker compose --env-file .env -f deploy/compose/docker-compose.yml up -d --build api
```

4. Проверить файл внутри контейнера:

```bash
docker exec nms1-api-1 ls -la /app/secrets/known_hosts
```

## Быстрая диагностика WS/SSH (2 команды)

После нажатия `Подключиться` в UI выполните:

```bash
docker logs --tail 120 nms1-api-1 2>&1 | grep 'nms-api: terminal-ws'
docker exec nms1-api-1 sh -lc 'tail -n 120 /app/logs/nms-api.log | grep -E "terminal ws|terminal session|ssh host key|ssh dial|upgrade failed|init read failed"'
```

Как интерпретировать:

- `terminal-ws upgrade FAILED` — проблема handshake (Origin/Host/proxy/URL).
- `terminal ws init read failed ... unexpected EOF` — браузер закрыл WS до `init` (JS/клиентская ошибка).
- `ssh host key policy` / `known_hosts` — не настроен файл host key или mismatch ключа.
- `ssh dial:` — TCP/SSH недоступен с хоста NMS до устройства.

## Дополнительно

Проверка reachability с сервера NMS:

```bash
nc -zv 192.168.10.10 22
nc -zv 192.168.0.10 23
```

Если свич заменили или ключ изменился, обновите строку в `.secrets/known_hosts` и перезапустите `api`.

