# NMS1 Production Readiness Checklist

Дата обновления: 2026-04-09

Этот файл фиксирует минимальные требования для безопасного go-live и текущий статус проекта.

Статусы:
- `[x]` — выполнено
- `[~]` — частично выполнено / есть оговорки
- `[ ]` — не выполнено

## 1) Security & Access

- [~] RBAC (admin/viewer) реализован на сервере
  - `RequireAdmin` стоит на mutating-роутах (`/devices`, `/discovery/scan`, `/snmp/set`, и т.д.).
  - UI для viewer скрыт (без discovery/add/edit/delete/SNMP SET/HTTP-HTTPS кнопок).
  - Оговорка: требуется регрессионный e2e/smoke тест на все сценарии ролей.

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

## 2) Secrets & Configuration

- [~] Конфиг через env
  - Учетки и сессия поддерживаются через `NMS_ADMIN_*`, `NMS_VIEWER_*`, `NMS_SESSION_SECRET`.
  - Оговорка: требуется обязательная политика ротации и хранение секретов вне `.env` в проде.

- [ ] Полный secret-management процесс
  - Vault / Docker secrets / аналог.
  - Ротация и отзыв старых секретов (особенно Telegram/DB/admin pass).

## 3) Data Safety (PostgreSQL)

- [x] Регулярные backup’ы БД
  - Добавлены скрипты backup + retention: `scripts/backup_postgres.sh`.
  - Проверен ручной backup: `2026-04-09` (`NMS_2026-04-09T16-05-48.dump` + checksum).

- [x] Проверка restore-процедуры
  - Добавлен restore-runbook и скрипт: `scripts/restore_postgres.sh`, `BACKUP_RESTORE.md`.
  - Выполнен restore drill: `2026-04-09` в БД `NMS_restore_test` (таблицы и данные проверены).

## 4) Observability & Operations

- [~] Метрики и healthcheck присутствуют
  - Есть `/metrics`, `/health`, worker metrics.
  - Оговорка: нет полного набора прод-алертов и SLO-гейтов.

- [ ] Алертинг (Alertmanager / Telegram route / и т.д.)
  - Алерты минимум на: API 5xx, недоступность worker, DB ошибки, рост failed polling.

- [~] Логирование присутствует
  - Есть логи сервисов и ротация worker-логов.
  - Оговорка: нужно формально проверить, что секреты не пишутся в логи.

## 5) Reliability

- [~] Worker устойчив к части ошибок
  - Есть backoff, классификация ошибок опроса, события доступности.
  - Оговорка: не выполнены fault-injection/chaos проверки.

- [ ] Runbook инцидентов
  - Нужны инструкции: “DB down”, “worker stalled”, “trap backlog”, “rollback deploy”.

## 6) Testing & Release Process

- [ ] Интеграционные тесты критичных сценариев
  - auth/rbac/device mutate/viewer read-only/worker interval setting.

- [ ] Деплойный smoke-test
  - Авто-проверка после релиза (login, devices list, worker heartbeat, DB connectivity).

- [ ] Rollback-процедура
  - Шаги отката версии и согласованная политика миграций.

---

## Минимальный Go-Live Gate (блокеры)

Перед production запуском обязательно закрыть:

- [x] CSRF для mutating-запросов
- [x] Rate-limit/lockout для `/login`
- [~] Security headers + HTTPS-only политика
- [x] Backup + проверенный restore
- [ ] Базовый набор алертов
- [ ] Smoke-test после деплоя

---

## Что уже хорошо в текущем состоянии

- Разделение ролей и серверная защита mutating API.
- Веб-авторизация с cookie-сессией и выходом.
- История доступности, health/metrics, worker backoff.
- Настраиваемый интервал опроса устройств через UI.

Это хороший уровень для staging/UAT. Для production — нужен фокус на security hardening, backup/restore и эксплуатационных процессах.

