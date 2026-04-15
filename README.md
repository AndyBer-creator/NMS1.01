# NMS1 — Network Management System

SNMP-мониторинг: опрос устройств, приём трапов, метрики в PostgreSQL, алерты в Telegram, дашборд и Prometheus/Grafana.

Операционные материалы (backup, runbook, секреты, SLO, go-live): каталог [`doc/`](doc/README.md). Целевой **enterprise**-уровень (readiness, OpenAPI, security.txt): [`doc/ENTERPRISE.md`](doc/ENTERPRISE.md).

## Требования

- Docker и Docker Compose
- Go **1.26+** (как в `go.mod`; для локальной сборки и миграций)

## Быстрый старт

1. **Секреты в .env**

   ```bash
   cp .env.example .env
   # Отредактируйте .env: пароль Postgres, DB_DSN, TELEGRAM_TOKEN, TELEGRAM_CHAT_ID, GRAFANA_ADMIN_PASSWORD
   ```

   Для production рекомендуется Docker-secrets overlay:

   ```bash
   make init-secrets
   docker compose -f docker-compose.yml -f docker-compose.secrets.yml up -d
   # или bridge-вариант:
   # docker compose -f docker-compose.bridge.yml -f docker-compose.secrets.yml up -d
   ```

2. **Миграции БД** (один раз, до первого запуска сервисов)

   ```bash
   docker compose up -d postgres
   # Дождитесь готовности БД, затем с хоста (в .env должен быть DB_DSN с host=localhost и вашим паролем):
   source .env   # или вручную export DB_DSN="..."
   go run ./cmd/migration
   ```

3. **Запуск стека**

   ```bash
   docker compose up -d
   ```

  По умолчанию **api** и **worker** в `network_mode: host` (весь стек в Docker, SNMP до LAN как с хоста — Linux или Docker Engine в WSL2). В `.env` пароль Postgres в `POSTGRES_PASSWORD` и в `DB_DSN` для migration/trap должен совпадать. `sslmode` для api/worker задаётся через `DB_SSLMODE` (default `disable` для локального single-host); для production используйте минимум `require` (лучше `verify-full`).

   Если **Docker Desktop для Windows** не поднимает сервисы с `network_mode: host`, используйте полностью bridge-стек:

   ```bash
   docker compose -f docker-compose.bridge.yml up -d
   ```

   и `DB_DSN` с `host=postgres` как в `.env.example`. Для SNMP до LAN в этом режиме включите в Docker Desktop **mirrored networking** (Settings → Resources → Network), если метрики/опрос снова не доходят до оборудования.

   В основном compose **Traefik** не проксирует api по Docker labels (api в host-сети); вход — **http://localhost:8080**. В `docker-compose.bridge.yml` labels Traefik для api сохранены.

  Production profile (строгие overrides поверх основного compose):

  ```bash
  # Рекомендуемо вместе с secrets overlay
  docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.secrets.yml up -d
  ```

  `docker-compose.prod.yml` включает production-safe defaults:
  - `NMS_ENV=production`;
  - `NMS_ENFORCE_HTTPS=true`;
  - явный запрет insecure-flags (`NMS_ALLOW_NO_AUTH=false`, `NMS_TERMINAL_ALLOW_INSECURE_* = false`, `NMS_SMTP_ALLOW_PLAINTEXT=false`);
  - `DB_SSLMODE=require` для api/worker;
  - Traefik ACME переключён на production Let's Encrypt CA (`TRAEFIK_ACME_EMAIL` должен быть задан).

- **API:** http://localhost:8080 (дашборд, устройства, трапы, health, `/metrics` для Prometheus)
- **Grafana:** http://localhost:3000 (логин/пароль из `GRAFANA_ADMIN_*` в .env)
- **Prometheus:** http://localhost:9090

### Grafana dashboards (provisioned)

- `nms-api-metrics` — общий API/worker observability (RPS, error ratio, latency, LLDP/worker runtime metrics).
- `nms-incident-sla` — incident-focused SLA/operations (MTTA, MTTR, transitions throughput, bulk transition failed ratio).
- Опционально: задайте `NMS_GRAFANA_BASE_URL`, чтобы на dashboard UI появилась role-aware ссылка для admin на `nms-incident-sla`.

## Сервисы

| Сервис          | Назначение |
|-----------------|------------|
| **postgres**    | БД: устройства, метрики, трапы |
| **api**         | HTTP API, дашборд, Prometheus-метрики |
| **worker**      | Периодический SNMP-опрос устройств из БД, запись метрик |
| **trap-receiver** | Приём SNMP-трапов на UDP 162, запись в БД, критичные OID → Telegram |
| **prometheus**  | Сбор метрик (основной compose: `host.docker.internal:8080` / `:8081`; bridge-файл: `api:8080`, `worker:8081`) |
| **grafana**     | Визуализация (источники: Prometheus, при желании БД) |

## Конфигурация

- **config.yaml** — HTTP-порт, SNMP-таймауты (без секретов).
- **.env** — секреты: `DB_DSN`, `POSTGRES_PASSWORD`, `TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID`, `GRAFANA_ADMIN_PASSWORD`. См. `.env.example`.
- **docker-compose.secrets.yml** — overlay для чтения секретов через `*_FILE` из `/.secrets` (рекомендуется для production).
- **config/critical_oids.json** — список критичных OID для алертов в Telegram (используется trap-receiver). Путь переопределяется через `CRITICAL_OIDS_FILE`.
- **NMS_TRUSTED_PROXIES** — список trusted proxy (CIDR/IP через запятую): только от них принимаются `X-Forwarded-For`/`X-Real-IP`/`X-Forwarded-Proto` (default: loopback).

### Production guardrails

Если `NMS_ENV=production`, сервисы запускаются только при безопасной конфигурации:

- обязательны `NMS_SESSION_SECRET` и `NMS_DB_ENCRYPTION_KEY`;
- `NMS_SESSION_SECRET` в production должен быть не короче 12 символов;
- `NMS_DB_ENCRYPTION_KEY` в production должен быть не короче 8 символов;
- обязателен `NMS_TERMINAL_SSH_KNOWN_HOSTS` (проверка host key для web-terminal SSH);
- `DB_DSN` должен явно задавать безопасный TLS-режим: `sslmode=require|verify-ca|verify-full`;
- `DB_DSN` в production должен включать непустые `user`, `host` и `dbname` (для URL-формы — user, host и путь БД);
- `DB_DSN` (если указан `port`) должен использовать валидный порт `1..65535`;
- обязателен `NMS_ENFORCE_HTTPS=true`;
- запрещены insecure-флаги: `NMS_ALLOW_NO_AUTH`, `NMS_TERMINAL_ALLOW_INSECURE_ORIGIN`, `NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY`, `NMS_SMTP_ALLOW_PLAINTEXT`.
- SMTP-конфиг (если включён) должен быть полным и валидным: `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`; `SMTP_PORT` — число `1..65535`, в production допускаются только `465` (SMTPS) или `587` (STARTTLS), а `SMTP_FROM` должен быть валидным email.
- если используется SMTP-аутентификация, `SMTP_USER` и `SMTP_PASS` должны быть заданы вместе (или оба пустые).
- `SMTP_HOST` должен быть чистым host/IP (без схемы `smtp://`, URL-пути и встроенного порта; порт задаётся только через `SMTP_PORT`).
- если используются `*_FILE` для `NMS_SESSION_SECRET`/`NMS_DB_ENCRYPTION_KEY`/`DB_DSN`/`SMTP_*`, пути должны быть абсолютными, а файлы — существовать и быть непустыми; `NMS_TERMINAL_SSH_KNOWN_HOSTS` должен указывать на абсолютный путь к доступному непустому файлу с хотя бы одной валидной записью host key.

Веб-интерфейс подключает **htmx**, **Tailwind** (собранный CSS) и **vis-network** с **`/static`**, без CDN в рантайме. Готовые файлы лежат в `static/css/app.css` и `static/js/*.js` и попадают в Docker-образ как есть. После правок классов в `templates/` или `internal/**/*.go` пересоберите CSS: `make static-css` (нужны Node.js и npm; зависимости — `npm ci`). Перед коммитом можно проверить, что закоммиченный `app.css` совпадает с билдом: `make check-static-css` (тот же CI-шаг). Обновить версии vendor-JS: `./scripts/fetch_vendor_js.sh` или `make vendor-js`.

Смена URL API (**IP → id** устройства): [`doc/API_MIGRATION.md`](doc/API_MIGRATION.md).

## Backup / Restore (PostgreSQL)

- Ручной backup: `./scripts/backup_postgres.sh` (архив `.dump` + `.sha256`, retention по дням)
- Ручной restore: `./scripts/restore_postgres.sh <file.dump> [target_db]`
- DR цели: `RPO <= 60m`, `RTO <= 120m`
- Offsite/immutable hooks:
  - `BACKUP_OFFSITE_SYNC_CMD`
  - `BACKUP_IMMUTABLE_COPY_CMD`
- Restore drill log:
  - `RESTORE_DRILL_LOG=./logs/restore-drill.tsv`
- Подробный runbook: [`doc/BACKUP_RESTORE.md`](doc/BACKUP_RESTORE.md)

## Operations docs

Каталог [`doc/`](doc/README.md):

- [`doc/RUNBOOK.md`](doc/RUNBOOK.md) — диагностика и действия при инцидентах.
- [`doc/ROLLBACK.md`](doc/ROLLBACK.md) — пошаговая процедура отката релиза/БД.
- [`doc/SECRETS_POLICY.md`](doc/SECRETS_POLICY.md) — политика хранения и ротации секретов.
- [`doc/SECRETS_PROCESS.md`](doc/SECRETS_PROCESS.md) — пошаговый процесс Docker-secrets: bootstrap, rotation, revoke.
- [`doc/GO_LIVE_CHECKLIST.md`](doc/GO_LIVE_CHECKLIST.md) — краткий чеклист перед выводом в эксплуатацию (команды из `Makefile` и ссылки на доки).
- [`doc/PROD_CHECKLIST.md`](doc/PROD_CHECKLIST.md) — статус production readiness.

## Alerting (Prometheus + Alertmanager + Email/Telegram)

- Базовые правила: `alerts/nms-alerts.yml`
  - включая worker-правила на spikes по failed/backoff и на slow polling cycle;
- Alertmanager конфиг:
  - `alertmanager.yml` (основной compose, webhook в `host.docker.internal:8080`)
  - `alertmanager.bridge.yml` (bridge compose, webhook в `api:8080`)
- Поток уведомлений:
  - Prometheus rules -> Alertmanager -> `POST /alerts/webhook` (api)
  - API отправляет best-effort в Telegram (если заданы `TELEGRAM_*`) и Email (если заданы SMTP env + email получателя в UI).

SMTP env:
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`
- `NMS_SMTP_ALLOW_PLAINTEXT` — legacy override для небезопасного SMTP без STARTTLS (по умолчанию выключен).

## Smoke test после деплоя

- Запуск: `make smoke-test` или `./scripts/smoke_test.sh`
- Проверяет базовые эндпоинты:
  - `/health` (liveness)
  - `/ready` (readiness, JSON)
  - `/metrics` (api)
  - `/login`
  - `/devices/list` (если заданы `NMS_ADMIN_USER/NMS_ADMIN_PASS`)
  - `/events/availability/page`
  - `http://localhost:8081/metrics` (worker)

## Проверка логов на утечку секретов

- Запуск: `make log-secrets-check` или `./scripts/check_logs_no_secrets.sh`
- Проверяет:
  - отсутствие в `logs/` ключевых названий чувствительных переменных;
  - отсутствие точных значений секретов из текущего окружения.

## SLO-gates (Prometheus)

- Запуск: `make slo-gates` или `./scripts/check_slo_gates.sh`
- Документ порогов и выражений: [`doc/SLO_GATES.md`](doc/SLO_GATES.md)
- Гейты включают API 5xx ratio и worker-критерии: failed count/ratio, backoff skips, avg poll cycle duration.
- Валидация Prometheus alert rules: `make alert-rules-check` (`promtool check rules` + `promtool test rules` через Docker).
- По умолчанию в основном compose Prometheus не публикуется на хост (`expose`, без `ports`).
- Рекомендуемо для production: запускать через ingress-домен:

  ```bash
  PROM_URL=https://prom.nms.test.com make slo-gates
  ```

- Для локальной отладки можно временно открыть `9090` через override:

  ```bash
  docker compose -f docker-compose.yml -f docker-compose.prom-host.yml up -d prometheus
  make slo-gates
  ```

  `docker-compose.prom-host.yml` использовать только для dev/debug.

## HTTPS-only policy

- Включение (production): `NMS_ENFORCE_HTTPS=true`.
- Поведение:
  - plain HTTP перенаправляется на HTTPS (`308`);
  - `/health`, `/ready`, `/metrics` и `/.well-known/security.txt` остаются доступными по HTTP для probe/scrape/security.txt (при терминации TLS на ingress).
- Проверка: `make https-policy-check`.

## Worker chaos check (fault-injection)

- Безопасный сценарий (по умолчанию): `make chaos-worker-check`
  - принудительно завершает процесс `worker` (`SIGKILL`) и проверяет восстановление сервиса + доступность worker metrics (с автоперезапуском или reconcile через `docker compose up -d worker`).
- Расширенный сценарий с кратковременным outage БД:

  ```bash
  CHAOS_DB_OUTAGE=true CHAOS_DB_OUTAGE_SECONDS=20 make chaos-worker-check
  ```

## RBAC smoke test

- Запуск: `make rbac-smoke` или `./scripts/rbac_smoke_test.sh`
- Проверяет:
  - viewer получает `403` на admin-only POST роуты;
  - admin не блокируется на тех же обработчиках.

## Права доступа (RBAC)

Если задана хотя бы одна пара логин/пароль (`NMS_ADMIN_*` или `NMS_VIEWER_*`), включается вход:

- Страница **`/login`** — форма логина и пароля, после успешного входа выдаётся **cookie-сессия** (7 суток).
- Дополнительно поддерживается **HTTP Basic** (удобно для `curl`/скриптов с теми же учётными данными).

Роли:

- **admin** — полный доступ (создание/удаление устройств, SNMP SET, discovery scan и т.д.)
- **viewer** — только просмотр таблицы устройств/состояния и страниц (без изменений)

Переменные окружения:

- `NMS_ADMIN_USER`, `NMS_ADMIN_PASS`
- `NMS_VIEWER_USER`, `NMS_VIEWER_PASS`
- `NMS_SESSION_SECRET` — секрет подписи cookie (для production обязателен).
- `NMS_ALLOW_NO_AUTH=true` включает legacy no-auth режим (только для локальной отладки, не для production).
- `NMS_TERMINAL_SSH_KNOWN_HOSTS` — путь к known_hosts для проверки host key в web-terminal SSH.
- `NMS_DB_ENCRYPTION_KEY` — ключ шифрования SNMP credential-полей в PostgreSQL.
- `NMS_DB_ENCRYPTION_OLD_KEY` — временный старый ключ для процедуры re-encrypt (`go run ./cmd/rotate-db-secrets`), после ротации должен быть удалён из окружения.
- `NMS_TRUSTED_PROXIES` — trusted proxy boundary для forwarded headers (`X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`).

## HTTP API (REST)

### Добавление устройства (v2c / v3)

Эндпоинт: `POST /devices`

Обычно для `v2c` достаточно `community`.

Пример для `v2c`:

```json
{
  "ip": "192.0.2.10",
  "name": "switch-1",
  "community": "public",
  "snmp_version": "v2c"
}
```

Пример для `v3`:

```json
{
  "ip": "192.0.2.10",
  "name": "switch-1",
  "community": "snmpv3-user",
  "snmp_version": "v3",
  "auth_proto": "SHA",
  "auth_pass": "auth-secret",
  "priv_proto": "AES",
  "priv_pass": "priv-secret"
}
```

Примечание: в текущей схеме БД поле `community` используется как username для SNMPv3 (так устроено хранение/подключение в коде).

### GET SNMP metric (SNMP GET)

Эндпоинт: `GET /devices/{id}/metric/{oid}` — `{id}` это **числовой** `id` устройства в БД (стабильно для IPv6; IP в путь не кладём).

Возвращает значение для одного OID (OID должен быть числовым).

### SNMP SET (управление оборудованием)

Эндпоинт: `POST /devices/{id}/snmp/set`

Тело запроса (JSON):

```json
{
  "oid": "1.3.6.1.2.1.1.5.0",
  "type": "OctetString",
  "value": "my-value"
}
```

Поддерживаемые `type`:
`Null`, `Integer`, `OctetString`, `Counter32`, `Counter64`, `Gauge32`, `Uinteger32`, `TimeTicks`, `IPAddress`, `ObjectIdentifier`.

### Traps (JSON)

`GET /traps?limit=100` — последние трапы. Фильтр по устройству (по `id` в БД, удобно для IPv6): `GET /traps?device_id=42&limit=100`.

### Incidents (JSON)

- `GET /incidents?limit=100&status=new&severity=critical`
- `GET /incidents/{incidentID}`
- `POST /incidents` (admin)
- `POST /incidents/{incidentID}/status` (admin)
- UI: `GET /incidents/page`

Lifecycle: `new -> acknowledged -> in_progress -> resolved -> closed`.

Assignment:
- `POST /incidents/{incidentID}/assignee` (admin) — assign/unassign owner.
- Optional auto-assignment via env policy:
  - `NMS_INCIDENT_ASSIGNEE_CRITICAL`
  - `NMS_INCIDENT_ASSIGNEE_TRAP` / `NMS_INCIDENT_ASSIGNEE_POLLING` / `NMS_INCIDENT_ASSIGNEE_MANUAL`
  - `NMS_INCIDENT_ASSIGNEE_DEFAULT`
- Optional auto-escalation for unacknowledged incidents (`status=new`) in worker:
  - `NMS_INCIDENT_ESCALATION_ACK_TIMEOUT` (e.g. `15m`)
  - `NMS_INCIDENT_ASSIGNEE_ESCALATION` (target on-call/escalation queue)
  - `NMS_INCIDENT_ESCALATION_CHECK_INTERVAL` (default `1m`)
  - metrics: `nms_incident_escalations_total`, `nms_incident_escalation_ack_timeout_seconds`
- Escalation policy v2 (optional):
  - Stage2 escalation: `NMS_INCIDENT_ESCALATION_STAGE2_ACK_TIMEOUT`, `NMS_INCIDENT_ESCALATION_STAGE2_ASSIGNEE`
  - Stage1 severity override: `NMS_INCIDENT_ESCALATION_CRITICAL_ACK_TIMEOUT`, `NMS_INCIDENT_ESCALATION_CRITICAL_ASSIGNEE`
  - Stage1 source overrides: `NMS_INCIDENT_ESCALATION_{TRAP|POLLING|MANUAL}_ACK_TIMEOUT` + `_ASSIGNEE`
  - per-policy metric: `nms_incident_escalations_policy_total{policy=...}`

ITSM inbound sync:
- `POST /itsm/inbound` (public endpoint, token-protected) updates incident `status` and/or `assignee`.
- Auth: `Authorization: Bearer <NMS_ITSM_INBOUND_TOKEN>` or `X-ITSM-Token`.
- If token env is not configured, endpoint is disabled (`503`).
- Inbound mapping v2:
  - DB table `itsm_inbound_mappings` maps external fields (`provider/status/priority/owner`) to NMS `status/assignee`.
  - direct request values have priority; mapping fills missing `status/assignee`.
  - provider default can be set via `NMS_ITSM_INBOUND_PROVIDER` (fallback: `NMS_ITSM_PROVIDER`).

Dashboard external health badges (admin):
- Grafana source: `NMS_GRAFANA_BASE_URL`
- Prometheus source: `NMS_PROMETHEUS_BASE_URL` (fallback: `PROMETHEUS_BASE_URL`)
- Dashboard shows `up` / `degraded` / `down` / `not_configured`.

### Correlation / dedup

- Trap-receiver создаёт/обновляет open incident в suppression-window:
  - `NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW` (default `10m`).
- Polling:
  - при `unavailable` создаёт/touch-ит incident с дедупликацией;
  - при восстановлении auto-resolve open incidents источника `polling`.
  - `NMS_POLL_INCIDENT_SUPPRESSION_WINDOW` (default `10m`).

### Тест алерта в Telegram (исправленный endpoint)

Эндпоинт: `POST /test-alert`

Тело запроса (JSON):

```json
{
  "device_ip": "192.168.88.1",
  "oid": "1.3.6.1.4.1.9.9.41.1.2.3",
  "trap_vars": "Interface GigabitEthernet0/1 DOWN"
}
```

Если передать `trap_vars` не указано, используется `message`.

### Discovery по сети (SNMP scan)

Эндпоинт: `POST /discovery/scan` (роль **admin**). Тело — JSON с полем `cidr` и при необходимости `community`, `snmp_version`, `tcp_prefilter`, `auto_add` и т.д.

При нуле ответов поле `found` в ответе — пустой массив `[]`, а не `null`; в поле `hints` возвращаются типичные причины.

Если коммутаторы в подсети отвечают по SNMP, а скан всё равно пустой:

- **Community и версия** — по умолчанию v2c и `public`; укажите реальные `community` и при необходимости `snmp_version` / поля v3 (`auth_proto`, `auth_pass`, …).
- **Где запущен API** — в основном `docker-compose.yml` api/worker уже в **host-сети** (всё в Docker). На **bridge** (`docker-compose.bridge.yml`) включите в Docker Desktop **mirrored networking** или используйте Linux/WSL2 с основным compose.
- **Файрвол/ACL** — с IP хоста NMS до коммутаторов должен проходить **UDP/161** к SNMP-агенту.
- **`tcp_prefilter: true`** — хосты без открытых типичных TCP-портов не проверяются; для «чистого» SNMP оставьте `tcp_prefilter: false` (или не передавайте).

### Docker и SNMP (LAN)

**Основной** `docker-compose.yml`: **api** и **worker** в **`network_mode: host`** — весь стек остаётся в Docker, а исходящий SNMP к вашей LAN (например `192.168.0.100`) идёт с **сетевого стека хоста** (удобно на **Linux** и при **Docker Engine в WSL2**). Подключение к Postgres у api/worker задаётся в compose: `127.0.0.1:5432`, пароль из `POSTGRES_PASSWORD` (должен совпадать с паролем в `DB_DSN` для migration/trap), `sslmode` из `DB_SSLMODE`. Для production включите TLS (`DB_SSLMODE=require` или `verify-full`). `prometheus.yml` скрейпит метрики с `host.docker.internal:8080` и `:8081`.

**Docker Desktop для Windows** часто **не поддерживает** `network_mode: host` для Linux-контейнеров. Тогда используйте **только** bridge-файл (всё так же в Docker):

```bash
docker compose -f docker-compose.bridge.yml up -d
```

Файл `docker-compose.bridge.yml` монтирует **`prometheus.bridge.yml`** (targets `api:8080`, `worker:8081`). Для доступа SNMP к LAN в этом режиме на Windows включите **mirrored networking** в настройках Docker Desktop (Resources → Network), иначе симптомы те же: **failed** в UI и пустой discovery при рабочем SNMP с хоста.

1. **Проверка с хоста:** `snmpget -v2c -c … 192.168.0.100 1.3.6.1.2.1.1.1.0`.
2. **Если с хоста OK, а из NMS нет** — сравните режим compose (host vs bridge) и настройки Docker Desktop выше.

## Private MIBs (`.mib` файлы)

**API** переводит символьные имена в числовые OID через **`snmptranslate`** (в Docker-образе установлен **net-snmp-tools**). Каталоги поиска задаются в **`config.yaml`** (`paths.mib_upload_dir`, опционально `paths.mib_search_dirs`) и по умолчанию включают `mibs/uploads`, `mibs/public`, `mibs/vendor` (плюс системные MIB при наличии в образе).

- Загрузка файлов через дашборд (блок «MIB-файлы», роль admin) → `mibs/uploads/`.
- **`POST /mibs/resolve`** — тело JSON `{"symbol":"IF-MIB::sysDescr.0"}` → ответ с полем `oid`.
- **`GET /devices/{id}/metric/{oid}`** и **`POST /devices/{id}/snmp/set`** принимают в `oid` **числовой OID** или **символьное имя** (в URL для GET спецсимволы в OID кодируйте).

Периодический опрос **worker** использует только **числовые OID** из `internal/config/oids.go` (без MIB-парсера на лету).

Для больших флотов можно ограничить нагрузку worker:
- `NMS_WORKER_POLL_CONCURRENCY` — число параллельных опросов устройств в одном цикле (по умолчанию `4`, max `128`);
- `NMS_WORKER_POLL_RATE_LIMIT_PER_SEC` — ограничение запуска опросов в секундах (по умолчанию `0`, без лимита; max `1000`).
В `/metrics` worker дополнительно публикуются: `nms_worker_poll_skipped_backoff_total`, `nms_worker_poll_config_concurrency`, `nms_worker_poll_config_rate_limit_per_sec`.

Локально без Docker: установите **net-snmp** (чтобы в `PATH` был `snmptranslate`) или ограничьтесь числовыми OID.

## Локальная разработка

```bash
# БД (например только Postgres)
docker compose up -d postgres

# Локальное время в логах и UI (опционально)
export TZ=Europe/Moscow

export DB_DSN="host=localhost port=5432 user=nms-user password=YOUR_PASS dbname=NMS sslmode=disable"
go run ./cmd/migration
go run ./cmd/server    # API на :8080
go run ./cmd/worker    # в другом терминале
go run ./cmd/trap-receiver  # при необходимости (порт 162 — права)
```

## Тесты

### Юнит-тесты

```bash
go test ./... -count=1
# или (работает и без файла .env):
make test
```

БД не обязательна: интеграционные тесты **пропускаются**, если не задан `DB_DSN`.

Дополнительно локально:

```bash
make test-race    # go test -race ./... (как в CI)
make test-cover   # coverage.out + итоговая строка go tool cover -func
make lint           # golangci-lint (конфиг .golangci.yml), как в CI
make vuln           # govulncheck ./...
make check-coverage # нужен coverage.out (или сначала make test-cover)
make ci-local        # lint + vuln + gosec + compose + alert-rules + shell-syntax + tool-version-policy + go test -race + coverage gate
make sbom            # SPDX JSON SBOM в ./sbom.spdx.json (через docker + syft)
make e2e-http-smoke  # лёгкий HTTP e2e smoke (/health, /ready, /metrics)
make e2e-auth-smoke  # e2e smoke с login/cookie/RBAC на защищённых маршрутах
make contract-http-spec # contract smoke для /api/openapi.yaml и /.well-known/security.txt
make openapi-breaking-check # strict OpenAPI backward-compatibility vs origin/main (requires git history + docker)
make load-http-readonly  # нагрузка /health + /metrics (нужен API)
make k6-readonly         # то же через k6 (нужен бинарник k6)
make k6-session-csrf     # k6: Basic viewer + cookie CSRF + POST /mibs/resolve (нужны учётки viewer в env)
make k6-logout-csrf      # k6: Basic viewer + cookie CSRF + POST /logout (mutating, без изменения БД)
make k6-admin-csrf       # k6: Basic admin + CSRF + POST /devices (валидация 400, без INSERT)
```

Локальный **smoke по HTTP** (должен быть запущен API, по умолчанию `http://127.0.0.1:8080`):

```bash
make e2e-http-smoke
# или: BASE_URL=http://localhost:8080 ./scripts/e2e_http_smoke.sh
```

**Нагрузка (read-only)** на `/health` и `/metrics` — только при работающем API (по умолчанию 200 запросов, параллельность 25):

```bash
make load-http-readonly
# LOAD_REQUESTS=500 LOAD_CONCURRENCY=40 BASE_URL=http://127.0.0.1:8080 ./scripts/load_http_readonly.sh
```

**k6** (установите [k6](https://k6.io/docs/get-started/installation/)): `make k6-readonly` или `BASE_URL=http://127.0.0.1:8080 K6_VUS=40 K6_DURATION=1m k6 run scripts/k6_readonly.js`. Если `command -v k6` пустой, а бинарник лежит в **`~/.local/bin/k6`**, откройте новый терминал или выполните **`source ~/.bashrc`**; цели **`make k6-readonly`**, **`make k6-session-csrf`**, **`make k6-logout-csrf`**, **`make k6-admin-csrf`** сами подставляют **`~/.local/bin`** в `PATH` на время запуска.

**k6 с сессией и CSRF** (`scripts/k6_session_csrf.js`): в каждой итерации VU делает `GET /devices` с Basic (viewer) и заголовком `Accept: application/json`, получает cookie `nms_csrf`, затем `POST /mibs/resolve` с телом `{"symbol":"1.3.6.1.2.1.1.1.0"}` и заголовком `X-CSRF-Token` (числовой OID резолвится без snmptranslate). Ожидается **200** и JSON с полем `oid` — так проверяется double-submit cookie без мутаций БД и без «ложных» failed по HTTP из‑за 403. Запуск: экспортируйте пароли viewer (`K6_VIEWER_USER`, `K6_VIEWER_PASS` или `NMS_VIEWER_USER` / `NMS_VIEWER_PASS`, если `make` подхватил `.env`) и выполните `make k6-session-csrf` или `k6 run scripts/k6_session_csrf.js`.

**k6 CSRF на mutating-роуте** (`scripts/k6_logout_csrf.js`): в каждой итерации VU делает `GET /devices` (получает cookie `nms_csrf`), затем `POST /logout` с `X-CSRF-Token` и Basic. Ожидается **302** и `Location` начинающийся с `/login`. Это проверка CSRF на POST без изменения БД. Запуск: `make k6-logout-csrf`.

**k6 admin-only + CSRF** (`scripts/k6_admin_csrf.js`): Basic **admin**, `GET /devices` → `POST /devices` с телом `{}` и `X-CSRF-Token`. Обработчик требует поля устройства и отвечает **400** (запись в БД не выполняется). Порог **`http_req_failed` не используется**: в k6 ответы **4xx** по умолчанию считаются «failed» на уровне HTTP-метрики, хотя для этого сценария **400 — ожидаемый** успех проверки. Запуск: `export K6_ADMIN_USER` / `K6_ADMIN_PASS` (или `NMS_ADMIN_*`) и `make k6-admin-csrf`.

В CI:

- **`.github/workflows/test.yml`** — **push**/**pull_request**/**workflow_dispatch**: **lint**, **vuln**, **unit** (race, coverage, порог), **integration** (Postgres). Ручной полный прогон: **Actions → test → Run workflow**. У GitHub отключаются **расписания** после длительной неактивности репозитория (~60 дней без коммитов).
- **`.github/workflows/nightly-lite.yml`** — **ежедневно в 04:15 UTC**: **lint**, **govulncheck**, **alert-rules**, **tool-version-policy** (без тестов и БД). Полный набор тестов по расписанию не гоняется, чтобы экономить минуты раннеров; при необходимости верните `schedule` в `test.yml` или добавьте отдельный workflow.
- **`.github/workflows/promote.yml`** — ручной **stage → prod promotion**: фиксирует продвигаемый SHA, требует approvals через environments `stage` и `prod`, умеет прогонять `e2e_http_smoke.sh` по `STAGE_BASE_URL`/`PROD_BASE_URL` (или inputs), публикует `promotion-manifest` и `rollback-handoff`.

- **lint** — **golangci-lint** v2.6.1 (`golangci/golangci-lint-action`, настройки в **`.golangci.yml`**);
- **vuln** — **`govulncheck ./...`**; локально для деталей по «уязвимости в зависимостях»: `go run golang.org/x/vuln/cmd/govulncheck@v1.2.0 -show verbose ./...`;
- **gosec** — SAST для Go-кода (зафиксированная версия `v2.25.0` в CI/Makefile);
- **trivy** — filesystem supply-chain scan по репозиторию (HIGH/CRITICAL, `ignore-unfixed`);
- **sbom-sign** — генерация **SPDX JSON SBOM** (`sbom.spdx.json`) через **Syft**, keyless подпись + verify через **Cosign** (для push/manual), публикация артефактов `sbom-spdx` и `sbom-signature`;
- **compose-security** — policy-check для `docker-compose*.yml` и `Dockerfile` (запрещены `privileged: true`, `:latest`, disabled healthchecks);
- **alert-rules** — проверка `alerts/nms-alerts.yml` через `promtool check rules` + rule-unit-tests `alerts/nms-alerts.test.yml` (обязательный CI gate);
- **shell-syntax** — `bash -n` проверка всех `scripts/*.sh` (обязательный CI gate);
- **tool-version-policy** — запрет плавающих версий инструментов (`@latest`, mutable `uses:@main/@master`) в workflow/Makefile.
- **unit** — тесты с **`-race`**, покрытие, порог **`scripts/check_coverage.sh`** (по умолчанию **25%**, переменная `MIN_COVERAGE_PERCENT`), загрузка в **Codecov** (ошибка загрузки не валит job), артефакт **`coverage-out`**; при push в ту же ветку предыдущий прогон этого workflow **отменяется** (`concurrency`);
- **integration** — миграции, Postgres, тесты `Integration` с **`-race`**.
- **e2e-http-smoke** — обязательный HTTP smoke в CI с реальным запуском API (`/health`, `/ready`, `/metrics`) на ephemeral runner.
- **e2e-auth-smoke** — обязательный auth-aware smoke в CI: login admin/viewer, cookie-сессия, доступ к `/devices`, проверка `403` для viewer на admin-only UI route.
- **contract-http-spec** — обязательная contract-проверка встроенных `openapi.yaml` и `security.txt` через поднятый API (включая проверку, что `openapi.yaml` требует auth, а `security.txt` доступен анонимно).
- **openapi-breaking** — strict breaking-change check спецификации относительно `main` (`scripts/check_openapi_breaking.sh`, `oasdiff` в Docker; только для `pull_request`).

Для корректной работы approval-гейтов в promotion workflow настройте в GitHub Environments:
- `stage` и `prod` с Required reviewers;
- при необходимости Repository Variables: `STAGE_BASE_URL`, `PROD_BASE_URL` (например `https://nms-stage.example.com`, `https://nms.example.com`).

Обновления зависимостей: **Dependabot** (`.github/dependabot.yml`) — еженедельно `gomod` и **GitHub Actions**. Для **приватного** репозитория в Codecov обычно задают секрет **`CODECOV_TOKEN`** (Settings → Secrets → Actions); без токена загрузка отчёта может быть нестабильной, в CI это не валит job.

Логи **worker** и **trap-receiver** (и пакет **`pkg/logger`**, если используете): каталог **`NMS_LOG_DIR`** переопределяет путь; иначе в Docker (`NMS_ENV=docker`) — `/app/logs`, локально — `./logs`.

Дополнительно без БД покрыты: **`internal/applog`** (каталог логов и zap-файл), **`pkg/logger`** (logrus + `NMS_LOG_DIR`), **`internal/services`** (SMTP `Enabled` и проверки до сети; Telegram — `httptest`, в проде по-прежнему `http.DefaultClient`), **`internal/usecases/discovery`** и **`internal/usecases/lldp`** (разбор OID/параметров), **`cmd/migration`** — наличие каталога **`migrations/`**; **`cmd/server`** — сборка router, `run()` (слушатель, shutdown), **`TestMain`** переключает cwd на корень модуля (шаблоны); **`cmd/worker`** — `run()` (SIGINT/SIGTERM, опциональный HTTP `/metrics`, SNMP/LLDP-циклы), тесты на metrics и отмену контекста; **`cmd/trap-receiver`** — `run()` (ping БД, UDP listener, `Close` по контексту), юнит-тесты DSN/ping/`TRAP_PORT`, интеграция graceful shutdown при доступном **`DB_DSN`** (как у HTTP: при `host=postgres` на хосте — **Skip** после неудачного ping). В **pull request** ориентируйтесь на зелёный workflow **test**.

### Интеграционные тесты (PostgreSQL)

Реальное подключение к БД с применёнными **миграциями**. Используйте тот же **`DB_DSN`**, что для `go run ./cmd/migration` и API (часто из `.env`). Если `go test` запускается **на хосте**, а в `.env` для Docker указан `host=postgres`, тесты **пропустят** интеграцию после неудачного ping (`internal/testdb`: **`PingDBOrSkip`** / **`PingDSNOrSkip`**); для реального прогона с хоста задайте `DB_DSN` с `localhost` / `127.0.0.1`.

```bash
source .env   # или вручную: export DB_DSN="..."
go test ./internal/infrastructure/postgres/ ./internal/repository/ -count=1 -v
# вместе с HTTP-интеграцией:
make test-integration
```

Покрытие: жизненный цикл устройств и метрик, настройки worker/email, события доступности, SNMP SET audit, LLDP scan/link, вставка и выборка трапов, лимит **`ByDevice`**. Тестовые устройства создаются с IP из диапазона `172.19.0.0/16`, чтобы не пересекаться с сидом `192.168.0.100` в миграции.

### HTTP-интеграция (Chi + реальная БД)

Пакет `internal/delivery/http`: полный `Router`, `httptest`, те же `DB_DSN` и миграции. Перед прогоном `TestMain` переключает рабочий каталог на **корень модуля** (нужны `templates/` и `static/`).

```bash
source .env
go test ./internal/delivery/http/ -count=1 -v -run Integration
```

Сценарии: `/health`, `/metrics`, JSON `GET /devices` и `GET /traps` (без обязательной авторизации — в тестах сбрасываются `NMS_ADMIN_*` / `NMS_VIEWER_*` и соответствующие `*_FILE`); **POST + DELETE `/devices`** под admin + CSRF; **401** на `POST /devices` без Basic при включённом admin; **403** для **viewer** на **`POST /devices`**, **`GET /devices/{id}/edit`**, **`POST /devices/{id}`** (обновление), **`DELETE /devices/{id}`**, **`POST /devices/{id}/snmp/set`**, **`POST /discovery/scan`**, **`POST /settings/worker-poll-interval`**, **`POST /settings/alert-email`**, **`POST /mibs/delete`**, **`POST /mibs/upload`** (multipart), **`POST /test-alert`**; **403** при неверном **`X-CSRF-Token`**; admin **POST `/settings/worker-poll-interval`** (**`interval_sec=333`**) и **POST `/settings/alert-email`** (валидный **`email`**) с откатом в **`t.Cleanup`**; невалидный **`email`** у admin → **200** и HTML с текстом валидации, **`GetAlertEmailTo`** без изменений. Общий сетап БД + router: **`buildIntegrationHandler`** / **`newIntegrationServer`**, **`applyIntegrationAuthEnv`**, **`newIntegrationHTTPClient`**, **`viewerIntegrationCSRF`** / **`adminIntegrationCSRF`** (seed **`GET /devices`** с Basic viewer/admin). Устройства — IP из `192.0.2.0/24` (TEST-NET-1) или `testDeviceIP`.

См. также разделы **Smoke test после деплоя** и **RBAC smoke test** выше (скрипты `scripts/smoke_test.sh`, `scripts/rbac_smoke_test.sh`).

## Лицензия

Проприетарный проект.
