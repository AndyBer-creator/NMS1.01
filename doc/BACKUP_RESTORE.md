# NMS1 Backup & Restore (PostgreSQL)

Этот документ описывает минимальный operational runbook для backup/restore БД в docker-compose окружении.

## 1) Ручной backup

```bash
./scripts/backup_postgres.sh
```

По умолчанию:
- backup dir: `./backups/postgres`
- формат: `pg_dump -Fc` (`.dump`)
- checksum: `.sha256`
- retention: `14` дней

Переопределение через env:

```bash
BACKUP_DIR=/data/nms-backups \
BACKUP_RETENTION_DAYS=30 \
COMPOSE_FILE=docker-compose.yml \
./scripts/backup_postgres.sh
```

## 2) Ручной restore

```bash
./scripts/restore_postgres.sh ./backups/postgres/NMS_YYYY-mm-ddTHH-MM-SS.dump
```

или в отдельную БД для проверки:

```bash
./scripts/restore_postgres.sh ./backups/postgres/NMS_YYYY-mm-ddTHH-MM-SS.dump NMS_restore_test
```

Что делает restore-скрипт:
- (если есть) проверяет checksum;
- завершает активные подключения к target DB;
- дропает и создаёт target DB;
- выполняет `pg_restore`.

## 3) Рекомендованный cron (ежедневно 03:15)

```cron
15 3 * * * cd /home/admin1/NMS1.01 && /usr/bin/env BACKUP_RETENTION_DAYS=30 ./scripts/backup_postgres.sh >> /home/admin1/NMS1.01/logs/backup.log 2>&1
```

## 4) Проверка восстановления (обязательно)

Минимум раз в месяц:
1. Сделать свежий backup.
2. Восстановить в тестовую БД (`NMS_restore_test`).
3. Проверить:
   - таблицы/данные доступны;
   - API поднимается на этой БД (в тестовом контуре);
   - ключевые экраны (`/devices/list`, `/events/availability/page`) читают данные.
4. Задокументировать дату и результат проверки.

## 5) Ограничения / замечания

- Скрипты рассчитаны на docker-compose с сервисом `postgres`.
- Для production желательно хранить бэкапы вне хоста (S3/NAS) и шифровать архивы.
- `POSTGRES_PASSWORD` должен быть надёжным и регулярно ротироваться.

