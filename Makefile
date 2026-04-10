SHELL := /bin/bash

.PHONY: migrate server worker traps dev docker-up clean backup-db restore-db smoke-test rbac-smoke init-secrets log-secrets-check slo-gates https-policy-check chaos-worker-check test test-race test-cover test-integration lint vuln check-coverage e2e-http-smoke load-http-readonly k6-readonly k6-session-csrf ci-local

# Если .env есть — подхватываем (docker, migrate, smoke). Без файла цели вроде `make test` всё равно работают.
ifneq (,$(wildcard .env))
include .env
export
endif

migrate:
	go run ./cmd/migration/

server:
	go run ./cmd/server/

worker:
	go run ./cmd/worker/

traps:
	go run ./cmd/trap-receiver/

dev:
	$(MAKE) migrate && $(MAKE) server & sleep 3 && $(MAKE) worker &

docker-up:
	sudo docker compose up -d postgres grafana

docker-logs:
	sudo docker compose logs -f

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

https-policy-check:
	./scripts/check_https_policy.sh

chaos-worker-check:
	./scripts/chaos_worker_check.sh

test:
	go test ./... -count=1

# Как в CI (см. .golangci.yml).
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.1 run ./... --timeout=5m

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check-coverage:
	@test -f coverage.out || $(MAKE) test-cover
	./scripts/check_coverage.sh coverage.out

e2e-http-smoke:
	./scripts/e2e_http_smoke.sh

# Нагрузка на /health и /metrics (нужен запущенный API). LOAD_REQUESTS, LOAD_CONCURRENCY, BASE_URL.
load-http-readonly:
	./scripts/load_http_readonly.sh

# Нагрузка k6 на /health и /metrics (нужен k6 и запущенный API). BASE_URL, K6_VUS, K6_DURATION.
k6-readonly:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 not found: https://k6.io/docs/get-started/installation/"; exit 1; }
	k6 run scripts/k6_readonly.js

# k6: сессия Basic + CSRF (GET /devices, POST /mibs/resolve). Нужны K6_VIEWER_USER / K6_VIEWER_PASS (или NMS_VIEWER_* в env).
k6-session-csrf:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 not found: https://k6.io/docs/get-started/installation/"; exit 1; }
	k6 run scripts/k6_session_csrf.js

# Локальная проверка перед пушем (без интеграции с БД): lint, vuln, тесты -race, порог coverage.
ci-local: lint vuln
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
test-integration:
	go test ./internal/infrastructure/postgres/ ./internal/repository/ ./internal/delivery/http/ -count=1 -v -run Integration
