# NMS1 SLO Gates

Формальные SLO-gates перед/после релиза (проверка через Prometheus API).

## Текущие гейты

- `API up`: `max(up{job="nms-api"}) >= 1`
- `Worker up`: `max(up{job="nms-worker"}) >= 1`
- `API 5xx ratio (5m) <= 5%`:
  - `sum(rate(nms_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(nms_requests_total[5m])), 1) <= 0.05`
- `Worker poll failures increase (10m) <= 20`:
  - `increase(nms_worker_poll_devices_total{status="failed"}[10m]) <= 20`
- `Worker poll failure ratio (15m) <= 30%`:
  - `sum(increase(nms_worker_poll_devices_total{status="failed"}[15m])) / clamp_min(sum(increase(nms_worker_poll_devices_total{status=~"failed|active"}[15m])), 1) <= 0.30`
- `Worker backoff skips increase (15m) <= 50`:
  - `increase(nms_worker_poll_skipped_backoff_total[15m]) <= 50`
- `Worker avg poll cycle duration (15m) <= 180s`:
  - `sum(increase(nms_worker_poll_duration_seconds_sum[15m])) / clamp_min(sum(increase(nms_worker_poll_duration_seconds_count[15m])), 1) <= 180`

## Запуск

```bash
make slo-gates
```

Если Prometheus доступен не на `localhost:9090`:

```bash
PROM_URL=http://prometheus.example:9090 make slo-gates
```

Пороговые значения можно переопределить env-переменными:
- `SLO_API_5XX_RATIO_MAX` (default `0.05`)
- `SLO_WORKER_POLL_FAIL_10M_MAX` (default `20`)
- `SLO_WORKER_POLL_FAIL_RATIO_15M_MAX` (default `0.30`)
- `SLO_WORKER_BACKOFF_SKIPS_15M_MAX` (default `50`)
- `SLO_WORKER_POLL_CYCLE_AVG_SEC_15M_MAX` (default `180`)

## Назначение

Этот gate используется как минимальная формализованная проверка observability/SLO в go-live процессе.
