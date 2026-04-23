# NMS1 Secret-Management Process (Docker Secrets)

Этот документ фиксирует рабочий production-процесс: где хранить секреты, как ротировать и как делать отзыв.

## 1) Целевое состояние

- Секреты не лежат в compose-файлах и не печатаются в логах.
- Сервисы читают чувствительные значения через `*_FILE` (файлы в `/run/secrets`).
- Ротация и отзыв выполняются по стандартной процедуре с проверками.

Поддерживаемые переменные: `DB_DSN`, `NMS_ADMIN_USER/PASS`, `NMS_VIEWER_USER/PASS`, `NMS_SESSION_SECRET`, `TELEGRAM_TOKEN/CHAT_ID`, `SMTP_USER/PASS/FROM`, `NMS_ALERT_WEBHOOK_TOKEN`, `NMS_ITSM_INBOUND_TOKEN`, `NMS_GRPC_AUTH_TOKEN` (для `trap-receiver -> api` gRPC ingest).
Опционально (если включаете TLS/mTLS для gRPC): сертификаты/ключи хранятся отдельными файлами в `.secrets` и передаются через `*_FILE` переменные.

## 2) Bootstrap

1. Заполнить `.env` один раз (как источник для генерации файлов).
2. Сгенерировать файлы секретов:

```bash
make init-secrets
```

`make init-secrets` создаёт/обновляет, в том числе, `.secrets/nms_grpc_auth_token` (из `NMS_GRPC_AUTH_TOKEN`, либо fallback на `NMS_TRAP_GRPC_AUTH_TOKEN`).

3. Запустить стек с overlay:

```bash
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.secrets.yml up -d
```

Bridge-вариант:

```bash
docker compose -f deploy/compose/docker-compose.bridge.yml -f deploy/compose/docker-compose.secrets.yml up -d
```

## 3) Rotation (плановая)

1. Сгенерировать новые значения (пароли/токены/ключи), включая `NMS_GRPC_AUTH_TOKEN` при включенном gRPC forwarding.
2. Для ротации `NMS_DB_ENCRYPTION_KEY` сначала подготовить старый и новый ключ:

```bash
export NMS_DB_ENCRYPTION_OLD_KEY='<старый ключ>'
export NMS_DB_ENCRYPTION_KEY='<новый ключ>'
```

Проверить dry-run и затем применить re-encrypt:

```bash
go run ./cmd/rotate-db-secrets --dry-run
go run ./cmd/rotate-db-secrets
# или make rotate-db-secrets
```

3. Обновить `.env`/Docker secrets только на новый ключ и пересоздать `.secrets`:

```bash
make init-secrets
```

4. Перезапустить только затронутые сервисы:

```bash
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.secrets.yml up -d api worker trap-receiver migration
```

Если ротировали только gRPC токен/сертификаты, достаточно:

```bash
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.secrets.yml up -d api trap-receiver
```

5. Проверить работоспособность:

```bash
make smoke-test
make rbac-smoke
```

## 4) Revoke (внеплановый отзыв при инциденте)

1. Немедленно выпустить новые креды у провайдера (SMTP/Telegram/DB и т.д.) и новый `NMS_GRPC_AUTH_TOKEN` (если включён gRPC ingest).
2. Обновить `.env` и выполнить `make init-secrets`.
3. Перезапустить сервисы с overlay и убедиться, что старые секреты невалидны.
4. Проверить `/health`, логин, alert pipeline и smoke-тесты.
5. Зафиксировать инцидент и время отзыва в ops-журнале.

## 5) Минимальные операционные правила

- Каталог `.secrets/` хранится только на хосте, в git не коммитится.
- Права на каталог: `0700`, на файлы: `0600`.
- Доступ к хосту и бэкапам `.secrets` ограничен минимально необходимым кругом.
