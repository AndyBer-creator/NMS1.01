SHELL := /bin/bash

.PHONY: migrate server worker traps dev docker-up clean backup-db restore-db smoke-test rbac-smoke init-secrets

# Правильный способ: env файл для make
include .env
export

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
