# Enterprise posture (NMS1)

Документ фиксирует целевой уровень для организаций с формальными требованиями к эксплуатации, наблюдаемости и интеграциям. Это **не сертификат** (ISO/SOC2 и т.д.), а согласованный набор возможностей и практик в репозитории.

## Наблюдаемость и оркестрация

| Элемент | Назначение |
|---------|------------|
| **`GET /health`** | **Liveness** — процесс отвечает; не нагружать БД. |
| **`GET /ready`** | **Readiness** — `Ping` к PostgreSQL, таймаут 2 с; **503**, если БД недоступна. Использовать в балансировщиках и Kubernetes readinessProbe. |
| **`GET /metrics`** | Prometheus (API и отдельно worker). |
| **`X-Request-ID`** | Chi `middleware.RequestID` — корреляция запросов в логах прокси и приложения. |

## Безопасность и соответствие привычкам рынка

| Элемент | Назначение |
|---------|------------|
| **`GET /.well-known/security.txt`** | Контакт для отчётов об уязвимостях (RFC 9116). Отредактируйте `internal/delivery/http/spec/security.txt` и пересоберите образ. |
| **`GET /api/openapi.yaml`** | OpenAPI 3.0 после авторизации (cookie или Basic) — контракт для интеграторов и SAST/DAST. |
| **HTTPS-only** | `NMS_ENFORCE_HTTPS=true`; исключения: `/health`, `/ready`, `/metrics`, `/.well-known/security.txt` (совместимость с probe и security.txt по HTTP при терминации TLS на ingress). |
| **RBAC, CSRF, rate-limit login** | См. [PROD_CHECKLIST.md](PROD_CHECKLIST.md). |

### Дорожная карта (не автоматизировано в коде)

- Ужесточение **CSP** (отказ от `unsafe-inline` — вынос JS, nonce).
- Централизованные логи (ELK/OpenSearch), **аудит** админ-действий в отдельном потоке (сейчас — structured logs + аудит SNMP SET в БД).
- **mTLS** / API keys для машинных клиентов вместо Basic.

## Качество и релиз

| Элемент | Назначение |
|---------|------------|
| Порог **coverage** в CI | По умолчанию **20%** (`MIN_COVERAGE_PERCENT`, `scripts/check_coverage.sh`). |
| **static-css-sync** | Закоммиченный Tailwind совпадает с билдом. |
| **Интеграционные тесты** | PostgreSQL в CI; локально — `make test-integration`. |
| **Smoke / RBAC** | `make smoke-test`, `make rbac-smoke`. |

## Операции

- Чеклисты: [GO_LIVE_CHECKLIST.md](GO_LIVE_CHECKLIST.md), [RUNBOOK.md](RUNBOOK.md), [ROLLBACK.md](ROLLBACK.md).
- Миграция API на id устройств: [API_MIGRATION.md](API_MIGRATION.md).

## Связанные файлы в репозитории

- OpenAPI: `internal/delivery/http/spec/openapi.yaml` (встроен в бинарник через `go:embed`).
- security.txt: `internal/delivery/http/spec/security.txt` (также `go:embed`).
