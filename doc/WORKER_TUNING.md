# NMS1 Worker Polling Tuning (PromQL + Cheat Sheet)

Короткая памятка для настройки `NMS_WORKER_POLL_CONCURRENCY` и `NMS_WORKER_POLL_RATE_LIMIT_PER_SEC` по метрикам.

## 1) Готовые PromQL для Grafana

Используйте как панели (обычно range: 15m/1h):

- `Worker poll success rate (devices/s)`:
  - `sum(rate(nms_worker_poll_devices_total{status="active"}[5m]))`
- `Worker poll failed rate (devices/s)`:
  - `sum(rate(nms_worker_poll_devices_total{status="failed"}[5m]))`
- `Worker poll failure ratio (0..1)`:
  - `sum(increase(nms_worker_poll_devices_total{status="failed"}[15m])) / clamp_min(sum(increase(nms_worker_poll_devices_total{status=~"failed|active"}[15m])), 1)`
- `Worker backoff skips (15m)`:
  - `increase(nms_worker_poll_skipped_backoff_total[15m])`
- `Worker avg poll cycle duration (s, 15m)`:
  - `sum(increase(nms_worker_poll_duration_seconds_sum[15m])) / clamp_min(sum(increase(nms_worker_poll_duration_seconds_count[15m])), 1)`
- `Configured concurrency`:
  - `max(nms_worker_poll_config_concurrency)`
- `Configured rate-limit (start/s)`:
  - `max(nms_worker_poll_config_rate_limit_per_sec)`

## 2) Быстрый decision tree

- **Симптом:** `failure ratio` > 0.30 и растут `backoff skips`
  - **Действие:** снизить `NMS_WORKER_POLL_CONCURRENCY` на 25-50%.
  - **Если нужно:** задать/снизить `NMS_WORKER_POLL_RATE_LIMIT_PER_SEC` (например 20-50).

- **Симптом:** `avg poll cycle duration` растёт, но `failure ratio` низкий
  - **Действие:** поднять `NMS_WORKER_POLL_CONCURRENCY` на 25% (с шагами, не рывком).
  - **Проверка:** CPU/RAM хоста и стабильность БД/сети.

- **Симптом:** `failed rate` пики после увеличения concurrency
  - **Действие:** откатить предыдущее значение, включить/ужесточить `RATE_LIMIT_PER_SEC`.

- **Симптом:** `worker down` или cycle метрики не обновляются
  - **Действие:** runbook `RUNBOOK.md` (перезапуск worker, проверка БД и сети).

## 3) Безопасные стартовые диапазоны

- Малый флот (до ~100 устройств): `CONCURRENCY=4..8`, `RATE_LIMIT_PER_SEC=0..20`
- Средний (100-500): `CONCURRENCY=8..16`, `RATE_LIMIT_PER_SEC=20..80`
- Крупный (500+): `CONCURRENCY=16..32`, `RATE_LIMIT_PER_SEC=50..150`

Всегда повышать по шагам и смотреть динамику минимум 15-30 минут.

## 4) Пример apply

```bash
# пример для docker-compose окружения
export NMS_WORKER_POLL_CONCURRENCY=12
export NMS_WORKER_POLL_RATE_LIMIT_PER_SEC=40
docker compose -f deploy/compose/docker-compose.yml up -d worker
```

После изменения:
- убедиться, что `max(nms_worker_poll_config_concurrency)` и `max(nms_worker_poll_config_rate_limit_per_sec)` показывают ожидаемые значения;
- проверить, что `failure ratio` и `backoff skips` не ухудшились.
