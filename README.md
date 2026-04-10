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
- **docker-compose.secrets.yml** — overlay для чтения секретов через `*_FILE` из `/.secrets` (рекомендуется для production).
- **config/critical_oids.json** — список критичных OID для алертов в Telegram (используется trap-receiver). Путь переопределяется через `CRITICAL_OIDS_FILE`.

## Backup / Restore (PostgreSQL)

- Ручной backup: `./scripts/backup_postgres.sh` (архив `.dump` + `.sha256`, retention по дням)
- Ручной restore: `./scripts/restore_postgres.sh <file.dump> [target_db]`
- Подробный runbook: `BACKUP_RESTORE.md`

## Operations docs

- `RUNBOOK.md` — диагностика и действия при инцидентах.
- `ROLLBACK.md` — пошаговая процедура отката релиза/БД.
- `SECRETS_POLICY.md` — политика хранения и ротации секретов.
- `SECRETS_PROCESS.md` — пошаговый процесс Docker-secrets: bootstrap, rotation, revoke.
- `GO_LIVE_CHECKLIST.md` — краткий чеклист перед выводом в эксплуатацию (команды из `Makefile` и ссылки на доки).

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

## Проверка логов на утечку секретов

- Запуск: `make log-secrets-check` или `./scripts/check_logs_no_secrets.sh`
- Проверяет:
  - отсутствие в `logs/` ключевых названий чувствительных переменных;
  - отсутствие точных значений секретов из текущего окружения.

## SLO-gates (Prometheus)

- Запуск: `make slo-gates` или `./scripts/check_slo_gates.sh`
- Документ порогов и выражений: `SLO_GATES.md`
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
  - `/health` и `/metrics` остаются доступными по HTTP для probe/scrape совместимости.
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
make ci-local        # lint + vuln + go test -race + порог coverage (~2–4 мин)
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
- **`.github/workflows/nightly-lite.yml`** — **ежедневно в 04:15 UTC** только **lint** и **govulncheck** (без тестов и БД). Полный набор тестов по расписанию не гоняется, чтобы экономить минуты раннеров; при необходимости верните `schedule` в `test.yml` или добавьте отдельный workflow.

- **lint** — **golangci-lint** v2.6.1 (`golangci/golangci-lint-action`, настройки в **`.golangci.yml`**);
- **vuln** — **`govulncheck ./...`**; локально для деталей по «уязвимости в зависимостях»: `go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...`;
- **unit** — тесты с **`-race`**, покрытие, порог **`scripts/check_coverage.sh`** (по умолчанию **15%**, переменная `MIN_COVERAGE_PERCENT`), загрузка в **Codecov** (ошибка загрузки не валит job), артефакт **`coverage-out`**; при push в ту же ветку предыдущий прогон этого workflow **отменяется** (`concurrency`);
- **integration** — миграции, Postgres, тесты `Integration` с **`-race`**.

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

Сценарии: `/health`, `/metrics`, JSON `GET /devices` и `GET /traps` (без обязательной авторизации — в тестах сбрасываются `NMS_ADMIN_*` / `NMS_VIEWER_*` и соответствующие `*_FILE`); **POST + DELETE `/devices`** под admin + CSRF; **401** на `POST /devices` без Basic при включённом admin; **403** для **viewer** на **`POST /devices`**, **`GET /devices/{ip}/edit`**, **`POST /devices/{ip}`** (обновление), **`DELETE /devices/{ip}`**, **`POST /devices/{ip}/snmp/set`**, **`POST /discovery/scan`**, **`POST /settings/worker-poll-interval`**, **`POST /settings/alert-email`**, **`POST /mibs/delete`**, **`POST /mibs/upload`** (multipart), **`POST /test-alert`**; **403** при неверном **`X-CSRF-Token`**; admin **POST `/settings/worker-poll-interval`** (**`interval_sec=333`**) и **POST `/settings/alert-email`** (валидный **`email`**) с откатом в **`t.Cleanup`**; невалидный **`email`** у admin → **200** и HTML с текстом валидации, **`GetAlertEmailTo`** без изменений. Общий сетап БД + router: **`buildIntegrationHandler`** / **`newIntegrationServer`**, **`applyIntegrationAuthEnv`**, **`newIntegrationHTTPClient`**, **`viewerIntegrationCSRF`** / **`adminIntegrationCSRF`** (seed **`GET /devices`** с Basic viewer/admin). Устройства — IP из `192.0.2.0/24` (TEST-NET-1) или `testDeviceIP`.

См. также разделы **Smoke test после деплоя** и **RBAC smoke test** выше (скрипты `scripts/smoke_test.sh`, `scripts/rbac_smoke_test.sh`).

## Лицензия

Проприетарный проект.
