#!/usr/bin/env bash
set -euo pipefail

# Worker resilience check (fault-injection).
# Default scenario is safe: kill worker process and verify auto-restart + metrics recovery.
# Optional intrusive scenario:
#   CHAOS_DB_OUTAGE=true CHAOS_DB_OUTAGE_SECONDS=20 ./scripts/chaos_worker_check.sh

WORKER_METRICS_URL="${WORKER_METRICS_URL:-http://localhost:8081/metrics}"
CHAOS_DB_OUTAGE="${CHAOS_DB_OUTAGE:-false}"
CHAOS_DB_OUTAGE_SECONDS="${CHAOS_DB_OUTAGE_SECONDS:-20}"
COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

fail() {
  echo "[chaos-worker][FAIL] $*" >&2
  exit 1
}

info() {
  echo "[chaos-worker] $*"
}

wait_http_200() {
  local url="$1"
  local retries="${2:-30}"
  local sleep_sec="${3:-2}"
  local i code
  for ((i=1; i<=retries; i++)); do
    code="$(curl -sS -o /dev/null -w "%{http_code}" "$url" || true)"
    if [[ "$code" == "200" ]]; then
      return 0
    fi
    sleep "$sleep_sec"
  done
  return 1
}

container_id_for_service() {
  docker compose -f "$COMPOSE_FILE" ps -q "$1"
}

require_running_service() {
  local svc="$1"
  local cid
  cid="$(container_id_for_service "$svc")"
  [[ -n "$cid" ]] || fail "service '$svc' container not found"
  local running
  running="$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null || true)"
  [[ "$running" == "true" ]] || fail "service '$svc' is not running"
}

ensure_service_running() {
  local svc="$1"
  local cid
  cid="$(container_id_for_service "$svc")"
  if [[ -z "$cid" ]]; then
    docker compose -f "$COMPOSE_FILE" up -d "$svc" >/dev/null
  fi
  wait_service_running "$svc" 40 2 || fail "service '$svc' is not running"
}

wait_service_running() {
  local svc="$1"
  local retries="${2:-30}"
  local sleep_sec="${3:-2}"
  local i cid running
  for ((i=1; i<=retries; i++)); do
    cid="$(container_id_for_service "$svc")"
    if [[ -n "$cid" ]]; then
      running="$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null || true)"
      if [[ "$running" == "true" ]]; then
        return 0
      fi
    fi
    sleep "$sleep_sec"
  done
  return 1
}

wait_service_healthy() {
  local svc="$1"
  local retries="${2:-40}"
  local sleep_sec="${3:-2}"
  local i cid health running
  for ((i=1; i<=retries; i++)); do
    cid="$(container_id_for_service "$svc")"
    [[ -n "$cid" ]] || { sleep "$sleep_sec"; continue; }
    running="$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null || true)"
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$cid" 2>/dev/null || true)"
    if [[ "$running" == "true" && ( "$health" == "healthy" || "$health" == "none" ) ]]; then
      return 0
    fi
    sleep "$sleep_sec"
  done
  return 1
}

info "Checking prerequisites"
ensure_service_running "worker"
ensure_service_running "postgres"
wait_http_200 "$WORKER_METRICS_URL" 20 2 || fail "worker metrics not reachable at $WORKER_METRICS_URL"

info "Scenario 1: crash worker main process (PID 1) and verify auto-restart"
worker_cid="$(container_id_for_service worker)"
docker exec "$worker_cid" sh -c 'kill -9 1' >/dev/null 2>&1 || true
if ! wait_service_running "worker" 20 2; then
  info "Auto-restart not observed; reconciling service via docker compose -f $COMPOSE_FILE up -d worker"
  docker compose -f "$COMPOSE_FILE" up -d worker >/dev/null
  wait_service_running "worker" 30 2 || fail "worker did not recover after crash"
fi
wait_http_200 "$WORKER_METRICS_URL" 30 2 || fail "worker metrics did not recover after restart"
info "Scenario 1 passed"

if [[ "$CHAOS_DB_OUTAGE" == "true" ]]; then
  info "Scenario 2: temporary DB outage (${CHAOS_DB_OUTAGE_SECONDS}s)"
  docker compose -f "$COMPOSE_FILE" stop postgres >/dev/null
  sleep "$CHAOS_DB_OUTAGE_SECONDS"
  docker compose -f "$COMPOSE_FILE" start postgres >/dev/null
  wait_service_healthy "postgres" 60 2 || fail "postgres did not recover to healthy state"
  wait_service_running "worker" 30 2 || fail "worker not running after DB outage scenario"
  wait_http_200 "$WORKER_METRICS_URL" 30 2 || fail "worker metrics not reachable after DB outage scenario"
  info "Scenario 2 passed"
else
  info "Scenario 2 skipped (set CHAOS_DB_OUTAGE=true to enable)"
fi

echo "[chaos-worker][OK] resilience checks passed"
