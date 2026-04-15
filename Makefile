SHELL := /bin/bash

.PHONY: migrate rotate-db-secrets sbom server worker traps dev docker-up clean backup-db restore-db smoke-test rbac-smoke init-secrets log-secrets-check slo-gates alert-rules-check shell-syntax-check tool-version-check https-policy-check compose-security-check openapi-breaking-check chaos-worker-check test test-race test-cover test-integration lint vuln gosec check-coverage e2e-http-smoke e2e-auth-smoke contract-http-spec load-http-readonly k6-readonly k6-session-csrf k6-logout-csrf k6-admin-csrf ci-local static-css check-static-css vendor-js

# Если .env есть — подхватываем (docker, migrate, smoke). Без файла цели вроде `make test` всё равно работают.
ifneq (,$(wildcard .env))
include .env
export
endif

# Пересобрать Tailwind в static/css/app.css (нужны Node.js и npm).
static-css:
	npm ci
	npm run build:css

# Убедиться, что закоммиченный Tailwind совпадает с билдом (как в CI).
check-static-css:
	./scripts/check_static_css_sync.sh

# Обновить htmx / vis-network в static/js (версии см. scripts/fetch_vendor_js.sh).
vendor-js:
	./scripts/fetch_vendor_js.sh

migrate:
	go run ./cmd/migration/

rotate-db-secrets:
	go run ./cmd/rotate-db-secrets/

sbom:
	./scripts/generate_sbom.sh

server:
	go run ./cmd/server/

worker:
	go run ./cmd/worker/

traps:
	go run ./cmd/trap-receiver/

dev:
	$(MAKE) migrate && $(MAKE) server & sleep 3 && $(MAKE) worker &

docker-up:
	sudo docker compose -f deploy/compose/docker-compose.yml up -d postgres grafana

docker-logs:
	sudo docker compose -f deploy/compose/docker-compose.yml logs -f

clean:
	rm -rf bin/ *.log ./trap-receiver

backup-db:
	./scripts/backup_postgres.sh

restore-db:
	@if [ -z "$(FILE)" ]; then echo "Usage: make restore-db FILE=./backups/postgres/<file.dump> [DB=NMS_restore_test]"; exit 1; fi
	./scripts/restore_postgres.sh "$(FILE)" "$(or $(DB),)"

smoke-test:
	./scripts/smoke_test.sh

rbac-smoke:
	./scripts/rbac_smoke_test.sh

init-secrets:
	./scripts/init_docker_secrets.sh

log-secrets-check:
	./scripts/check_logs_no_secrets.sh

slo-gates:
	./scripts/check_slo_gates.sh

alert-rules-check:
	./scripts/check_alert_rules.sh

shell-syntax-check:
	./scripts/check_shell_syntax.sh

tool-version-check:
	./scripts/check_tool_versions.sh

https-policy-check:
	./scripts/check_https_policy.sh

compose-security-check:
	./scripts/check_compose_security.sh

openapi-breaking-check:
	./scripts/check_openapi_breaking.sh

chaos-worker-check:
	./scripts/chaos_worker_check.sh

test:
	go test ./... -count=1

# Как в CI (см. .golangci.yml).
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.1 run ./... --timeout=5m

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.2.0 ./...

gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@v2.25.0 ./...

check-coverage:
	@test -f coverage.out || $(MAKE) test-cover
	./scripts/check_coverage.sh coverage.out

e2e-http-smoke:
	./scripts/e2e_http_smoke.sh

e2e-auth-smoke:
	./scripts/e2e_auth_smoke.sh

contract-http-spec:
	./scripts/contract_http_spec.sh

# Нагрузка на /health и /metrics (нужен запущенный API). LOAD_REQUESTS, LOAD_CONCURRENCY, BASE_URL.
load-http-readonly:
	./scripts/load_http_readonly.sh

# Нагрузка k6 на /health и /metrics (нужен k6 и запущенный API). BASE_URL, K6_VUS, K6_DURATION.
# PATH дополняем ~/.local/bin (типичная установка без sudo), чтобы не зависеть от перезапуска терминала.
k6-readonly:
	@PATH="$(HOME)/.local/bin:$$PATH" command -v k6 >/dev/null 2>&1 || { echo "k6 not found (ожидался в PATH или $(HOME)/.local/bin): https://k6.io/docs/get-started/installation/"; exit 1; }
	PATH="$(HOME)/.local/bin:$$PATH" k6 run scripts/k6_readonly.js

# k6: сессия Basic + CSRF (GET /devices, POST /mibs/resolve). Нужны K6_VIEWER_USER / K6_VIEWER_PASS (или NMS_VIEWER_* в env).
k6-session-csrf:
	@PATH="$(HOME)/.local/bin:$$PATH" command -v k6 >/dev/null 2>&1 || { echo "k6 not found (ожидался в PATH или $(HOME)/.local/bin): https://k6.io/docs/get-started/installation/"; exit 1; }
	PATH="$(HOME)/.local/bin:$$PATH" k6 run scripts/k6_session_csrf.js

# k6: POST /logout под CSRF (mutating, без изменения БД). Нужны viewer учётки.
k6-logout-csrf:
	@PATH="$(HOME)/.local/bin:$$PATH" command -v k6 >/dev/null 2>&1 || { echo "k6 not found (ожидался в PATH или $(HOME)/.local/bin): https://k6.io/docs/get-started/installation/"; exit 1; }
	PATH="$(HOME)/.local/bin:$$PATH" k6 run scripts/k6_logout_csrf.js

# k6: admin Basic + CSRF + POST /devices (пустой JSON → 400, без INSERT в БД). Нужны admin учётки.
k6-admin-csrf:
	@PATH="$(HOME)/.local/bin:$$PATH" command -v k6 >/dev/null 2>&1 || { echo "k6 not found (ожидался в PATH или $(HOME)/.local/bin): https://k6.io/docs/get-started/installation/"; exit 1; }
	PATH="$(HOME)/.local/bin:$$PATH" k6 run scripts/k6_admin_csrf.js

# Локальная проверка перед пушем (без интеграции с БД): lint, vuln, gosec, compose policy, alert rules, shell syntax, pinned tools, тесты -race, порог coverage.
ci-local: lint vuln gosec compose-security-check alert-rules-check shell-syntax-check tool-version-check
	go test ./... -count=1 -race -coverprofile=coverage.out -covermode=atomic
	./scripts/check_coverage.sh coverage.out

# Как в CI job unit: детектор гонок (медленнее).
test-race:
	go test ./... -count=1 -race

# Профиль в ./coverage.out (в .gitignore); итог по функциям в консоль.
test-cover:
	go test ./... -count=1 -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out | tail -n 1

# Нужен DB_DSN (например из .env). Иначе сценарии Integration — SKIP.
# С хоста при Postgres на localhost: переопределите DB_DSN в командной строке (include .env из Makefile
# иначе задаст host=postgres из .env). Пример:
#   make test-integration DB_DSN='host=127.0.0.1 port=5432 user=nms-user password=YOUR_PASS dbname=NMS sslmode=disable'
test-integration:
	go test ./internal/infrastructure/postgres/ ./internal/repository/ ./internal/delivery/http/ -count=1 -v -run Integration
