# NMS1 Backup & Restore (PostgreSQL)

Этот документ описывает operational runbook для backup/restore БД в docker-compose окружении.

Целевые показатели DR:
- **RPO**: не хуже 60 минут.
- **RTO**: не хуже 120 минут (до восстановления API на целевой БД).

## 1) Ручной backup

```bash
./scripts/backup_postgres.sh
```

По умолчанию:
- backup dir: `./backups/postgres`
- формат: `pg_dump -Fc` (`.dump`)
- checksum: `.sha256`
- metadata: `.meta` (включая `rpo_target_minutes`/`rto_target_minutes`)
- retention: `14` дней

Переопределение через env:

```bash
BACKUP_DIR=/data/nms-backups \
BACKUP_RETENTION_DAYS=30 \
RPO_TARGET_MINUTES=60 \
RTO_TARGET_MINUTES=120 \
COMPOSE_FILE=deploy/compose/docker-compose.yml \
./scripts/backup_postgres.sh
```

Опциональные интеграции для offsite/immutable:

```bash
BACKUP_OFFSITE_SYNC_CMD='aws s3 cp "$BACKUP_FILE" s3://nms-dr-bucket/postgres/ && aws s3 cp "$BACKUP_SHA256_FILE" s3://nms-dr-bucket/postgres/ && aws s3 cp "$BACKUP_META_FILE" s3://nms-dr-bucket/postgres/' \
BACKUP_IMMUTABLE_COPY_CMD='aws s3 cp "$BACKUP_FILE" s3://nms-dr-immutable/postgres/ --storage-class DEEP_ARCHIVE --metadata lock=true' \
./scripts/backup_postgres.sh
```

## 2) Ручной restore

```bash
RESTORE_CONFIRM_DROP=YES \
./scripts/restore_postgres.sh ./backups/postgres/NMS_YYYY-mm-ddTHH-MM-SS.dump NMS_restore_test
```

`target_db` обязателен. Скрипт намеренно отказывается восстанавливать в primary/default БД (`POSTGRES_DB`/`NMS`) и требует явного подтверждения `RESTORE_CONFIRM_DROP=YES`, потому что делает destructive `DROP DATABASE` + `CREATE DATABASE`.

Пример для отдельной БД проверки:

```bash
RESTORE_CONFIRM_DROP=YES \
./scripts/restore_postgres.sh ./backups/postgres/NMS_YYYY-mm-ddTHH-MM-SS.dump NMS_restore_test
```

Что делает restore-скрипт:
- (если есть) проверяет checksum;
- показывает `.meta` (если найден);
- завершает активные подключения к target DB;
- дропает и создаёт target DB;
- выполняет `pg_restore`;
- пишет длительность restore в stdout.

Для регулярных restore-drill можно писать журнал:

```bash
RESTORE_CONFIRM_DROP=YES \
RESTORE_DRILL_LOG=./logs/restore-drill.tsv \
./scripts/restore_postgres.sh ./backups/postgres/NMS_YYYY-mm-ddTHH-MM-SS.dump NMS_restore_test
```

## 3) Рекомендованный cron (ежедневно 03:15)

```cron
15 3 * * * cd /home/admin1/NMS1.01 && /usr/bin/env BACKUP_RETENTION_DAYS=30 ./scripts/backup_postgres.sh >> /home/admin1/NMS1.01/logs/backup.log 2>&1
```

## 4) Restore drill (обязательно)

Минимум раз в месяц:
1. Сделать свежий backup.
2. Восстановить в тестовую БД (`NMS_restore_test`).
3. Проверить:
   - таблицы/данные доступны;
   - API поднимается на этой БД (в тестовом контуре);
   - ключевые экраны (`/devices/list`, `/events/availability/page`) читают данные.
4. Зафиксировать:
   - фактический `restore_duration_sec`;
   - прошли ли валидации smoke;
   - укладывается ли восстановление в `RTO`.
5. Задокументировать дату и результат проверки.

## 5) Ограничения / замечания

- Скрипты рассчитаны на docker-compose с сервисом `postgres`.
- Для production обязательно:
  - хранить бэкапы вне хоста (S3/NAS/объектное хранилище);
  - иметь immutable-копию (WORM/Object Lock/аналог);
  - шифровать архивы на уровне хранилища.
- `POSTGRES_PASSWORD` должен быть надёжным и регулярно ротироваться.

