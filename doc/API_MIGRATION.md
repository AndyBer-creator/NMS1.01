# Миграция HTTP API: IP в пути → `id` устройства

Раньше часть URL содержала **IP** устройства (`/devices/<ip>/…`, `/traps/<ip>`). Для **IPv6** и спецсимволов это неудобно и ломало маршрутизацию. Сейчас в пути используется **числовой `id`** из таблицы `devices`.

## Узнать `id`

- `GET /devices` (JSON) — у каждого элемента поле `id`.
- `POST /devices` (создание) — в ответе поле `id`.

## Замена URL

| Было | Стало |
|------|--------|
| `GET /devices/<ip>/metric/<oid>` | `GET /devices/<id>/metric/<oid>` |
| `POST /devices/<ip>/snmp/set` | `POST /devices/<id>/snmp/set` |
| `GET /devices/<ip>/edit` | `GET /devices/<id>/edit` |
| `POST /devices/<ip>` (форма/HTML) | `POST /devices/<id>` |
| `DELETE /devices/<ip>` | `DELETE /devices/<id>` |
| `GET /traps/<ip>?limit=…` | `GET /traps?device_id=<id>&limit=…` |
| `GET /traps?limit=…` (все) | без изменений |

Авторизация и CSRF — как раньше.

## Пример (curl)

```bash
# Список устройств → взять id
curl -sS -u 'admin:pass' -H 'Accept: application/json' https://nms.example/devices

# SNMP GET по id=7
curl -sS -u 'admin:pass' 'https://nms.example/devices/7/metric/1.3.6.1.2.1.1.1.0'

# Трапы только для устройства 7
curl -sS -u 'admin:pass' 'https://nms.example/traps?device_id=7&limit=100'
```

Подробности в [README.md](../README.md) (разделы HTTP API и Traps).
