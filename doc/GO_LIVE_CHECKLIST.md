# Go-live checklist (NMS1)

Краткий чеклист перед выводом в эксплуатацию. Команды и цели — из репозитория (`Makefile`, `scripts/`, корневой [`README.md`](../README.md)).

---

## 1. Код и качество (до тега/релиза)

| Шаг | Команда / действие |
|-----|-------------------|
| Локально как в CI | `make ci-local` (lint, vuln, тесты с `-race`, порог coverage) |
| Enterprise-чек (обзор) | [ENTERPRISE.md](ENTERPRISE.md) — `/ready` в балансировщике, правка `spec/security.txt`, выдача OpenAPI интеграторам |
| Статика Tailwind в git | После правок классов в шаблонах: `make static-css` и закоммитить `static/css/app.css`. Проверка: `make check-static-css` (тот же шаг, что job **static-css-sync** в CI). |
| Интеграция с PostgreSQL | Поднять БД, задать `DB_DSN`, затем `make test-integration`. С хоста при Postgres на `127.0.0.1` при необходимости переопределить `DB_DSN` в командной строке (см. комментарий в `Makefile` — `include .env` может задать `host=postgres`). |
| Политика HTTPS / SLO / хаос (если используете) | `make https-policy-check`, `make slo-gates`, `make chaos-worker-check` |
| Логи без секретов (после настройки логирования) | `make log-secrets-check` |

В CI уже есть: lint, **static-css-sync** (совпадение `static/css/app.css` с билдом Tailwind), `govulncheck`, unit с race+coverage (**порог 22%** по умолчанию), миграции + integration job (см. `.github/workflows/test.yml`), а также ручной promotion flow `stage -> prod` (см. `.github/workflows/promote.yml`).

---

## 2. Конфигурация и секреты (прод-стенд)

| Шаг | Действие |
|-----|----------|
| Секреты не в git | `.env` из `.env.example`, права на файлы; для Docker — `make init-secrets` и overlay `docker-compose.secrets.yml` (см. [`README.md`](../README.md), [`SECRETS_POLICY.md`](SECRETS_POLICY.md), [`SECRETS_PROCESS.md`](SECRETS_PROCESS.md)) |
| Сеть и режим compose | Выбрать основной compose или `docker-compose.bridge.yml` под вашу ОС/доступ SNMP к LAN (см. [`README.md`](../README.md)) |
| БД | `POSTGRES_PASSWORD` и `DB_DSN` согласованы; для миграций с хоста — `host=localhost` (или ваш хост БД) |

---

## 3. Миграции и сборка образов

| Шаг | Действие |
|-----|----------|
| Миграции до старта сервисов | `make migrate` или `go run ./cmd/migration` с корректным `DB_DSN` (как в [`README.md`](../README.md)) |
| Образы | `docker compose build` / `docker compose up -d --build` по вашему процессу релиза |

---

## 4. После выката (smoke)

| Шаг | Команда |
|-----|---------|
| Базовый HTTP / сессии | `make e2e-http-smoke` (нужен запущенный API; см. `scripts/e2e_http_smoke.sh`) |
| RBAC | `make rbac-smoke` |
| Ручной smoke (из [`RUNBOOK.md`](RUNBOOK.md)) | `curl` на `/health`, `/metrics` API и worker (`:8081`), `docker compose ps`, логи сервисов |

При необходимости нагрузочного дымка на read-only эндпоинты: `make load-http-readonly` или `make k6-readonly` (нужен запущенный API; для k6-сценариев с авторизацией — переменные из `Makefile`).

---

## 5. Резервное копирование и откат

| Шаг | Документ / команда |
|-----|-------------------|
| Процедура backup/restore | [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md); `make backup-db`, тестовый `make restore-db FILE=...` на копии |
| Откат релиза | [`ROLLBACK.md`](ROLLBACK.md) |
| Инциденты | [`RUNBOOK.md`](RUNBOOK.md) |

---

## 6. Наблюдаемость и алерты

| Шаг | Действие |
|-----|----------|
| Prometheus / Grafana | URL и datasource как в [`README.md`](../README.md); правила в `alerts/nms-alerts.yml`, Alertmanager — `alertmanager.yml` или `alertmanager.bridge.yml` |
| Проверка цепочки алертов | См. раздел в [`README.md`](../README.md) (Alerting) и [`RUNBOOK.md`](RUNBOOK.md) § про доставку уведомлений |

---

## 7. Команда и процесс

- Кто дежурит и где runbook ([`RUNBOOK.md`](RUNBOOK.md)).
- Согласованы ли требования по комплаенсу, хранению логов и данных SNMP/учёток в БД.

---

*Документ дополняет, а не заменяет [`README.md`](../README.md), [`RUNBOOK.md`](RUNBOOK.md) и [`ROLLBACK.md`](ROLLBACK.md).*
