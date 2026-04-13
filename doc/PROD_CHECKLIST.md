# NMS1 Production Readiness Checklist

Дата обновления: 2026-04-13 (enterprise: readiness, OpenAPI, security.txt, coverage 20%)

Этот файл фиксирует минимальные требования для безопасного go-live и текущий статус проекта.

Статусы:
- `[x]` — выполнено
- `[~]` — частично выполнено / есть оговорки
- `[ ]` — не выполнено

## 1) Security & Access

- [x] RBAC (admin/viewer) реализован на сервере
  - `RequireAdmin` стоит на mutating-роутах (`/devices`, `/discovery/scan`, `/devices/{id}/snmp/set`, и т.д.).
  - UI для viewer скрыт (без discovery/add/edit/delete/SNMP SET/кнопок HTTP/HTTPS/SSH/Telnet).
  - Добавлен и успешно прогнан регрессионный smoke: `scripts/rbac_smoke_test.sh` (`make rbac-smoke`, в т.ч. `GET /devices/{id}/edit` для viewer → 403).

- [x] Веб-логин реализован (`/login` + cookie session)
  - Есть logout и ограничение прав для viewer.
  - Добавлен rate-limit/lockout на попытки входа (по IP и username, с `429` и `Retry-After`).

- [x] CSRF защита для state-changing запросов
  - Включён middleware `RequireCSRF` (double-submit cookie) для mutating-методов.
  - Подключены токены в формы/HTMX/fetch (`X-CSRF-Token` / `csrf_token`).

- [x] Ограничение brute-force на login
  - Добавлен throttling/lockout по IP и username.
  - Логируются события `login failed` и `login throttled`.

- [x] Security headers
  - Добавлены: `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, `Content-Security-Policy`.
  - `Strict-Transport-Security` выставляется для HTTPS/`X-Forwarded-Proto=https`.
  - Введена HTTPS-only политика при `NMS_ENFORCE_HTTPS=true`: HTTP-запросы редиректятся на HTTPS (кроме `/health`, `/metrics`).

## 2) Secrets & Configuration

- [x] Конфиг через env
  - Учетки и сессия поддерживаются через `NMS_ADMIN_*`, `NMS_VIEWER_*`, `NMS_SESSION_SECRET`.
  - Добавлена формальная политика: `SECRETS_POLICY.md` (классификация, ротация, инцидент-процедура).

- [x] Полный secret-management процесс
  - Добавлена поддержка `*_FILE` для критичных секретов в коде (`DB_DSN`, auth/session, Telegram, SMTP).
  - Добавлен Docker-secrets overlay: `docker-compose.secrets.yml`.
  - Добавлен операционный процесс bootstrap/rotation/revoke: `SECRETS_PROCESS.md` + `scripts/init_docker_secrets.sh`.

## 3) Data Safety (PostgreSQL)

- [x] Регулярные backup’ы БД
  - Добавлены скрипты backup + retention: `scripts/backup_postgres.sh`.
  - Проверен ручной backup: `2026-04-09` (`NMS_2026-04-09T16-05-48.dump` + checksum).

- [x] Проверка restore-процедуры
  - Добавлен restore-runbook и скрипт: `scripts/restore_postgres.sh`, `BACKUP_RESTORE.md`.
  - Выполнен restore drill: `2026-04-09` в БД `NMS_restore_test` (таблицы и данные проверены).

## 4) Observability & Operations

- [x] Метрики и healthcheck присутствуют
  - Есть `/metrics`, `/health`, worker metrics.
  - Добавлены формальные SLO-gates и автоматическая проверка через Prometheus API: `SLO_GATES.md`, `scripts/check_slo_gates.sh` (`make slo-gates`).

- [x] Алертинг (Alertmanager / Telegram route / и т.д.)
  - Добавлены базовые Prometheus rules: `alerts/nms-alerts.yml` (API down, worker down, high 5xx, polling failures spike).
  - Добавлен Alertmanager в compose + webhook `POST /alerts/webhook` (Prometheus -> Alertmanager -> API).
  - Реализована доставка в Telegram (best-effort) и Email через SMTP (получатель настраивается в admin UI).
  - Подтверждена фактическая email-доставка (`2026-04-09`: письмо получено на целевой адрес).
  - Подтверждён e2e через Alertmanager -> webhook API -> email (`2026-04-09`).

- [x] Логирование присутствует
  - Есть логи сервисов и ротация worker-логов.
  - Добавлена формальная проверка на утечку секретов в логах: `scripts/check_logs_no_secrets.sh` (`make log-secrets-check`).

## 5) Reliability

- [x] Worker устойчив к части ошибок
  - Есть backoff, классификация ошибок опроса, события доступности.
  - Добавлен fault-injection скрипт `scripts/chaos_worker_check.sh` (`make chaos-worker-check`) для проверки auto-restart worker и восстановления метрик.

- [x] Runbook инцидентов
  - Добавлен `RUNBOOK.md` (API down, worker stalled, DB down, alert pipeline issues, smoke after incident).

## 6) Testing & Release Process

- [x] Интеграционные тесты критичных сценариев
  - HTTP: RBAC/CSRF (viewer vs admin), CRUD устройств, настройки worker/email, discovery/MIB/SNMP/test-alert; `internal/testdb` для ping БД; `make test-integration` и пакет `internal/delivery/http` (`-run Integration`).
  - PostgreSQL/traps: `internal/infrastructure/postgres`, `internal/repository` при `DB_DSN`.
  - CI: unit + integration (см. `.github/workflows/test.yml`), job **static-css-sync** (Tailwind `app.css` совпадает с билдом), порог покрытия по `scripts/check_coverage.sh` (по умолчанию **20%**).

## 7) Enterprise integration & ops hygiene

- [x] Разделение liveness / readiness
  - `GET /health` — без БД; `GET /ready` — проверка PostgreSQL (JSON **200** / **503**).
- [x] Корреляция запросов — заголовок **`X-Request-ID`** (Chi RequestID).
- [x] **security.txt** — `GET /.well-known/security.txt` (редактируемый шаблон в `internal/delivery/http/spec/security.txt`, исключение в HTTPS-only как у probes).
- [x] **OpenAPI 3** — `GET /api/openapi.yaml` после авторизации (встроенная спецификация).
- [x] Документ целевого уровня: [`ENTERPRISE.md`](ENTERPRISE.md).

- [x] Нагрузочные прогоны (k6)
  - Read-only: `make k6-readonly` (GET `/health` / `/metrics`).
  - Session+CSRF: `make k6-session-csrf` (viewer Basic → cookie `nms_csrf` → POST `/mibs/resolve` с `X-CSRF-Token`, ожидается 200 + JSON `oid`). Проверено 2026-04-10.
  - Admin+CSRF: `make k6-admin-csrf` (admin Basic → cookie `nms_csrf` → POST `/devices` с `{}`, ожидается 400 валидации без INSERT; порог только по `checks`, не по `http_req_failed`).

- [x] Деплойный smoke-test
  - Добавлен скрипт `scripts/smoke_test.sh` + цель `make smoke-test`.
  - Проверен успешный прогон: `2026-04-09`.

- [x] Rollback-процедура
  - Добавлен `ROLLBACK.md` (rollback приложения, rollback БД, pre-checks, политика миграций).

---

## Минимальный Go-Live Gate (блокеры)

Перед production запуском обязательно закрыть:

- [x] CSRF для mutating-запросов
- [x] Rate-limit/lockout для `/login`
- [x] Security headers + HTTPS-only политика
- [x] Backup + проверенный restore
- [x] Базовый набор алертов
- [x] Smoke-test после деплоя

---

## Что уже хорошо в текущем состоянии

- Разделение ролей и серверная защита mutating API.
- Веб-авторизация с cookie-сессией и выходом.
- История доступности, health/metrics, worker backoff.
- Настраиваемый интервал опроса устройств через UI.

Репозиторий закрывает заявленные **enterprise hygiene** пункты (см. §7 и `ENTERPRISE.md`). Для регулируемых сред дополнительно нужны организационные меры: комплаенс, DLP, SIEM, пентест и согласованные SLO/SLA.

