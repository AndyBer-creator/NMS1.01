# NMS1 — Network Management System

SNMP-мониторинг: опрос устройств, приём трапов, метрики в PostgreSQL, алерты в Telegram, дашборд и Prometheus/Grafana.

## Требования

- Docker и Docker Compose
- Go 1.25+ (для локальной сборки и миграций)

## Быстрый старт

1. **Секреты в .env**

   ```bash
   cp .env.example .env
   # Отредактируйте .env: пароль Postgres, DB_DSN, TELEGRAM_TOKEN, TELEGRAM_CHAT_ID, GRAFANA_ADMIN_PASSWORD
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

   По умолчанию **api** и **worker** в `network_mode: host` (весь стек в Docker, SNMP до LAN как с хоста — Linux или Docker Engine в WSL2). В `.env` пароль Postgres в `POSTGRES_PASSWORD` и в `DB_DSN` для migration/trap должен совпадать.

   Если **Docker Desktop для Windows** не поднимает сервисы с `network_mode: host`, используйте полностью bridge-стек:

   ```bash
   docker compose -f docker-compose.bridge.yml up -d
   ```

   и `DB_DSN` с `host=postgres` как в `.env.example`. Для SNMP до LAN в этом режиме включите в Docker Desktop **mirrored networking** (Settings → Resources → Network), если метрики/опрос снова не доходят до оборудования.

   В основном compose **Traefik** не проксирует api по Docker labels (api в host-сети); вход — **http://localhost:8080**. В `docker-compose.bridge.yml` labels Traefik для api сохранены.

- **API:** http://localhost:8080 (дашборд, устройства, трапы, health, `/metrics` для Prometheus)
- **Grafana:** http://localhost:3000 (логин/пароль из `GRAFANA_ADMIN_*` в .env)
- **Prometheus:** http://localhost:9090

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
- **config/critical_oids.json** — список критичных OID для алертов в Telegram (используется trap-receiver). Путь переопределяется через `CRITICAL_OIDS_FILE`.

## Backup / Restore (PostgreSQL)

- Ручной backup: `./scripts/backup_postgres.sh` (архив `.dump` + `.sha256`, retention по дням)
- Ручной restore: `./scripts/restore_postgres.sh <file.dump> [target_db]`
- Подробный runbook: `BACKUP_RESTORE.md`

## Operations docs

- `RUNBOOK.md` — диагностика и действия при инцидентах.
- `ROLLBACK.md` — пошаговая процедура отката релиза/БД.

## Alerting (Prometheus + Alertmanager + Email/Telegram)

- Базовые правила: `alerts/nms-alerts.yml`
- Alertmanager конфиг:
  - `alertmanager.yml` (основной compose, webhook в `host.docker.internal:8080`)
  - `alertmanager.bridge.yml` (bridge compose, webhook в `api:8080`)
- Поток уведомлений:
  - Prometheus rules -> Alertmanager -> `POST /alerts/webhook` (api)
  - API отправляет best-effort в Telegram (если заданы `TELEGRAM_*`) и Email (если заданы SMTP env + email получателя в UI).

SMTP env:
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`

## Smoke test после деплоя

- Запуск: `make smoke-test` или `./scripts/smoke_test.sh`
- Проверяет базовые эндпоинты:
  - `/health`
  - `/metrics` (api)
  - `/login`
  - `/devices/list` (если заданы `NMS_ADMIN_USER/NMS_ADMIN_PASS`)
  - `/events/availability/page`
  - `http://localhost:8081/metrics` (worker)

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
- `NMS_SESSION_SECRET` (опционально) — секрет подписи cookie; если не задан, ключ выводится из заданных пар (для продакшена лучше задать явно).

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

Эндпоинт: `GET /devices/{ip}/metric/{oid}`

Возвращает значение для одного OID (OID должен быть числовым).

### SNMP SET (управление оборудованием)

Эндпоинт: `POST /devices/{ip}/snmp/set`

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

**Основной** `docker-compose.yml`: **api** и **worker** в **`network_mode: host`** — весь стек остаётся в Docker, а исходящий SNMP к вашей LAN (например `192.168.0.100`) идёт с **сетевого стека хоста** (удобно на **Linux** и при **Docker Engine в WSL2**). Подключение к Postgres у api/worker задаётся в compose: `127.0.0.1:5432` и пароль из `POSTGRES_PASSWORD` (должен совпадать с паролем в `DB_DSN` для migration/trap). `prometheus.yml` скрейпит метрики с `host.docker.internal:8080` и `:8081`.

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
- **`GET /devices/{ip}/metric/{oid}`** и **`POST /devices/{ip}/snmp/set`** принимают в `oid` **числовой OID** или **символьное имя** (в URL для GET символы вроде `::` нужно кодировать).

Периодический опрос **worker** использует только **числовые OID** из `internal/config/oids.go` (без MIB-парсера на лету).

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

## Лицензия

Проприетарный проект.
