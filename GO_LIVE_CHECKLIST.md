# Go-live checklist (NMS1)

Краткий чеклист перед выводом в эксплуатацию. Команды и цели — из репозитория (`Makefile`, `scripts/`, `README.md`).

---

## 1. Код и качество (до тега/релиза)

| Шаг | Команда / действие |
|-----|-------------------|
| Локально как в CI | `make ci-local` (lint, vuln, тесты с `-race`, порог coverage) |
| Интеграция с PostgreSQL | Поднять БД, задать `DB_DSN`, затем `make test-integration`. С хоста при Postgres на `127.0.0.1` при необходимости переопределить `DB_DSN` в командной строке (см. комментарий в `Makefile` — `include .env` может задать `host=postgres`). |
| Политика HTTPS / SLO / хаос (если используете) | `make https-policy-check`, `make slo-gates`, `make chaos-worker-check` |
| Логи без секретов (после настройки логирования) | `make log-secrets-check` |

В CI уже есть: lint, `govulncheck`, unit с race+coverage, миграции + integration job (см. `.github/workflows/test.yml`).

---

## 2. Конфигурация и секреты (прод-стенд)

| Шаг | Действие |
|-----|----------|
| Секреты не в git | `.env` из `.env.example`, права на файлы; для Docker — `make init-secrets` и overlay `docker-compose.secrets.yml` (см. `README.md`, `SECRETS_POLICY.md`, `SECRETS_PROCESS.md`) |
| Сеть и режим compose | Выбрать основной compose или `docker-compose.bridge.yml` под вашу ОС/доступ SNMP к LAN (см. `README.md`) |
| БД | `POSTGRES_PASSWORD` и `DB_DSN` согласованы; для миграций с хоста — `host=localhost` (или ваш хост БД) |

---

## 3. Миграции и сборка образов

| Шаг | Действие |
|-----|----------|
| Миграции до старта сервисов | `make migrate` или `go run ./cmd/migration` с корректным `DB_DSN` (как в `README.md`) |
| Образы | `docker compose build` / `docker compose up -d --build` по вашему процессу релиза |

---

## 4. После выката (smoke)

| Шаг | Команда |
|-----|---------|
| Базовый HTTP / сессии | `make e2e-http-smoke` (нужен запущенный API; см. `scripts/e2e_http_smoke.sh`) |
| RBAC | `make rbac-smoke` |
| Ручной smoke (из `RUNBOOK.md`) | `curl` на `/health`, `/metrics` API и worker (`:8081`), `docker compose ps`, логи сервисов |

При необходимости нагрузочного дымка на read-only эндпоинты: `make load-http-readonly` или `make k6-readonly` (нужен запущенный API; для k6-сценариев с авторизацией — переменные из `Makefile`).

---

## 5. Резервное копирование и откат

| Шаг | Документ / команда |
|-----|-------------------|
| Процедура backup/restore | `BACKUP_RESTORE.md`; `make backup-db`, тестовый `make restore-db FILE=...` на копии |
| Откат релиза | `ROLLBACK.md` |
| Инциденты | `RUNBOOK.md` |

---

## 6. Наблюдаемость и алерты

| Шаг | Действие |
|-----|----------|
| Prometheus / Grafana | URL и datasource как в `README.md`; правила в `alerts/nms-alerts.yml`, Alertmanager — `alertmanager.yml` или `alertmanager.bridge.yml` |
| Проверка цепочки алертов | См. раздел в `README.md` (Alerting) и `RUNBOOK.md` § про доставку уведомлений |

---

## 7. Команда и процесс

- Кто дежурит и где runbook (`RUNBOOK.md`).
- Согласованы ли требования по комплаенсу, хранению логов и данных SNMP/учёток в БД.

---

*Документ дополняет, а не заменяет `README.md`, `RUNBOOK.md` и `ROLLBACK.md`.*
