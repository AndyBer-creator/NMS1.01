SHELL := /bin/bash

.PHONY: migrate server worker traps dev docker-up clean

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
	rm -rf bin/ *.log
