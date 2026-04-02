# NMS1 — Network Management System

SNMP-мониторинг: опрос устройств, приём трапов, метрики в PostgreSQL, алерты в Telegram, дашборд и Prometheus/Grafana.

## Требования

- Docker и Docker Compose
- Go 1.25+ (для локальной сборки и миграций)

## Быстрый старт

1. **Секреты в .env**

   ```bash
   cp .env.example .env
   # Отредактируйте .env: пароль Postgres, DB_DSN, TELEGRAM_TOKEN, TELEGRAM_CHAT_ID, GRAFANA_ADMIN_PASSWORD
   ```

2. **Миграции БД** (один раз, до первого запуска сервисов)

   ```bash
   docker compose up -d postgres
   # Дождитесь готовности БД, затем с хоста (в .env должен быть DB_DSN с host=localhost и вашим паролем):
   source .env   # или вручную export DB_DSN="..."
   go run ./cmd/migration
   ```

3. **Запуск стека**

   ```bash
   docker compose up -d
   ```

- **API:** http://localhost:8080 (дашборд, устройства, трапы, health, `/metrics` для Prometheus)
- **Grafana:** http://localhost:3000 (логин/пароль из `GRAFANA_ADMIN_*` в .env)
- **Prometheus:** http://localhost:9090

## Сервисы

| Сервис          | Назначение |
|-----------------|------------|
| **postgres**    | БД: устройства, метрики, трапы |
| **api**         | HTTP API, дашборд, Prometheus-метрики |
| **worker**      | Периодический SNMP-опрос устройств из БД, запись метрик |
| **trap-receiver** | Приём SNMP-трапов на UDP 162, запись в БД, критичные OID → Telegram |
| **prometheus**  | Сбор метрик (в т.ч. с api:8080/metrics) |
| **grafana**     | Визуализация (источники: Prometheus, при желании БД) |

## Конфигурация

- **config.yaml** — HTTP-порт, SNMP-таймауты (без секретов).
- **.env** — секреты: `DB_DSN`, `POSTGRES_PASSWORD`, `TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID`, `GRAFANA_ADMIN_PASSWORD`. См. `.env.example`.
- **config/critical_oids.json** — список критичных OID для алертов в Telegram (используется trap-receiver). Путь переопределяется через `CRITICAL_OIDS_FILE`.

## Права доступа (RBAC)

Включается HTTP Basic Auth. Есть две роли:

- **admin** — полный доступ (создание/удаление устройств, SNMP SET, discovery scan и т.д.)
- **viewer** — только просмотр таблицы устройств/состояния и страниц (без изменений)

Настраивается через переменные окружения:

- `NMS_ADMIN_USER`, `NMS_ADMIN_PASS`
- `NMS_VIEWER_USER`, `NMS_VIEWER_PASS`

## HTTP API (REST)

### Добавление устройства (v2c / v3)

Эндпоинт: `POST /devices`

Обычно для `v2c` достаточно `community`.

Пример для `v2c`:

```json
{
  "ip": "192.0.2.10",
  "name": "switch-1",
  "community": "public",
  "snmp_version": "v2c"
}
```

Пример для `v3`:

```json
{
  "ip": "192.0.2.10",
  "name": "switch-1",
  "community": "snmpv3-user",
  "snmp_version": "v3",
  "auth_proto": "SHA",
  "auth_pass": "auth-secret",
  "priv_proto": "AES",
  "priv_pass": "priv-secret"
}
```

Примечание: в текущей схеме БД поле `community` используется как username для SNMPv3 (так устроено хранение/подключение в коде).

### GET SNMP metric (SNMP GET)

Эндпоинт: `GET /devices/{ip}/metric/{oid}`

Возвращает значение для одного OID (OID должен быть числовым).

### SNMP SET (управление оборудованием)

Эндпоинт: `POST /devices/{ip}/snmp/set`

Тело запроса (JSON):

```json
{
  "oid": "1.3.6.1.2.1.1.5.0",
  "type": "OctetString",
  "value": "my-value"
}
```

Поддерживаемые `type`:
`Null`, `Integer`, `OctetString`, `Counter32`, `Counter64`, `Gauge32`, `Uinteger32`, `TimeTicks`, `IPAddress`, `ObjectIdentifier`.

### Тест алерта в Telegram (исправленный endpoint)

Эндпоинт: `POST /test-alert`

Тело запроса (JSON):

```json
{
  "device_ip": "192.168.88.1",
  "oid": "1.3.6.1.4.1.9.9.41.1.2.3",
  "trap_vars": "Interface GigabitEthernet0/1 DOWN"
}
```

Если передать `trap_vars` не указано, используется `message`.

## Private MIBs (как добавить свои `.mib` файлы)

Важно: текущий проект работает с **числовыми OID**. Private MIBs в основном нужны, чтобы:
- именами OID пользоваться “человечески” при настройке устройств;
- конвертировать `MY-MIB::myOid` в числовой `1.3.6...` до отправки в NMS (например, через `snmptranslate`/`snmpwalk` на вашей машине/в контейнере с SNMP утилитами).

Ниже варианты, которые обычно используют.

### Способ 1: монтировать MIB-ы в контейнер и держать `MIBDIRS`

1) Разместите свои файлы в папке, например: `./mibs/private/`
2) Смонтируйте в контейнер путь, где ожидают mibs (примерно `/usr/share/snmp/mibs`) и задайте `MIBDIRS`.

Пример для `docker-compose.yml` (кусок):

```yaml
services:
  api:
    volumes:
      - ./mibs/private:/usr/share/snmp/mibs:ro
    environment:
      - MIBDIRS=/usr/share/snmp/mibs
```

### Способ 2: использовать SNMP-утилиты на хосте

Если вы используете `snmptranslate/snmpwalk` на хосте, просто укажите `MIBDIRS`/`MIBS` в shell окружении:

```bash
export MIBDIRS=./mibs/private
# иногда требуется: export MIBS=+ALL
```

После чего конвертируйте именованные OID в числовые и отправляйте в NMS.

## Локальная разработка

```bash
# БД (например только Postgres)
docker compose up -d postgres

export DB_DSN="host=localhost port=5432 user=nms-user password=YOUR_PASS dbname=NMS sslmode=disable"
go run ./cmd/migration
go run ./cmd/server    # API на :8080
go run ./cmd/worker    # в другом терминале
go run ./cmd/trap-receiver  # при необходимости (порт 162 — права)
```

## Лицензия

Проприетарный проект.
