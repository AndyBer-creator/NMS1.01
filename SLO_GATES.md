# NMS1 SLO Gates

Формальные SLO-gates перед/после релиза (проверка через Prometheus API).

## Текущие гейты

- `API up`: `max(up{job="nms-api"}) >= 1`
- `Worker up`: `max(up{job="nms-worker"}) >= 1`
- `API 5xx ratio (5m) <= 5%`:
  - `sum(rate(nms_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(nms_requests_total[5m])), 1) <= 0.05`
- `Worker poll failures increase (10m) <= 20`:
  - `increase(nms_worker_poll_devices_total{status="failed"}[10m]) <= 20`

## Запуск

```bash
make slo-gates
```

Если Prometheus доступен не на `localhost:9090`:

```bash
PROM_URL=http://prometheus.example:9090 make slo-gates
```

## Назначение

Этот gate используется как минимальная формализованная проверка observability/SLO в go-live процессе.
