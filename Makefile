SHELL := /bin/bash

.PHONY: migrate server worker traps dev docker-up clean backup-db restore-db smoke-test rbac-smoke init-secrets log-secrets-check slo-gates https-policy-check chaos-worker-check test test-integration

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

# Нужен DB_DSN (например из .env). Иначе сценарии Integration — SKIP.
test-integration:
	go test ./internal/infrastructure/postgres/ ./internal/repository/ ./internal/delivery/http/ -count=1 -v -run Integration
