# NMS1 Runbook

Операционный runbook для дежурного инженера.

## 0) Базовые проверки

```bash
cd /home/admin1/NMS1.01
docker compose ps
docker compose logs --tail=100 api
docker compose logs --tail=100 worker
docker compose logs --tail=100 postgres
```

Проверки HTTP:

```bash
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8080/health
curl -sS http://localhost:8080/metrics | head
curl -sS http://localhost:8081/metrics | head
```

## 1) API недоступен / 5xx растут

Симптомы:
- `/health` не 200
- алерты `NMSApiDown` / `NMSHigh5xxRate`

Действия:
1. Проверить контейнер:
   ```bash
   docker compose ps api
   docker compose logs --tail=200 api
   ```
2. Перезапустить API:
   ```bash
   docker compose up -d --build api
   ```
3. Если не помогло — откатить API (см. `ROLLBACK.md`).

## 2) Worker stalled / polling не идёт

Симптомы:
- `nms_worker_poll_duration_seconds` перестал обновляться
- рост failed polling / нет новых событий доступности
- алерты `NMSPollingFailuresSpike`, `NMSPollingFailureRatioHigh`, `NMSPollingBackoffSpike`, `NMSPollingCycleSlow`

Действия:
1. Проверить worker:
   ```bash
   docker compose ps worker
   docker compose logs --tail=300 worker
   ```
2. Перезапуск:
   ```bash
   docker compose up -d --build worker
   ```
3. Проверить БД/доступность устройств/таймауты SNMP.
4. Если растёт `NMSPollingBackoffSpike`:
   - проверить network reachability до проблемных IP;
   - временно снизить `NMS_WORKER_POLL_CONCURRENCY` и/или `NMS_WORKER_POLL_RATE_LIMIT_PER_SEC`;
   - после стабилизации вернуть значения по SLO.
5. Для выбора target-значений и PromQL-панелей использовать `doc/WORKER_TUNING.md`.

## 3) PostgreSQL недоступна

Симптомы:
- API/worker ошибки подключения к БД
- `postgres` unhealthy

Действия:
1. Проверить postgres:
   ```bash
   docker compose ps postgres
   docker compose logs --tail=200 postgres
   ```
2. Если контейнер упал — поднять:
   ```bash
   docker compose up -d postgres
   ```
3. Если повреждение данных / авария — восстановление:
   см. `BACKUP_RESTORE.md`.

## 4) Alert pipeline не доставляет уведомления

Симптомы:
- есть firing alerts, но нет email/telegram

Действия:
1. Проверить Alertmanager:
   ```bash
   docker compose ps alertmanager
   docker compose logs --tail=200 alertmanager
   ```
2. Проверить API webhook:
   ```bash
   curl -sS -X POST http://localhost:8080/alerts/webhook \
     -H "Content-Type: application/json" \
     --data '{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"RunbookTest"},"annotations":{"summary":"runbook test","description":"manual"}}]}'
   docker compose logs --tail=100 api
   ```
3. Проверить SMTP/Telegram env и email в UI.

## 5) Быстрый smoke после инцидента

```bash
make smoke-test
make rbac-smoke
```

## 6) Эскалация

Эскалировать, если:
- повторный падёж сервиса после 2 рестартов;
- DB restore требуется срочно и есть риск потери данных;
- нет доставки алертов > 30 минут.

