# NMS1 Rollback Procedure

Цель: быстро откатить сервисы к последней стабильной версии.

## 1) Когда откатываться

- массовые 5xx после релиза;
- критичный функционал недоступен (login/devices/worker polling);
- ошибка миграции или несовместимость схемы.

## 2) До отката (обязательно)

1. Зафиксировать инцидент:
   - время начала;
   - какой релиз/коммит выкатили;
   - симптомы и метрики.
2. Снять логи:
   ```bash
   docker compose logs --tail=300 api > /tmp/nms_api_fail.log
   docker compose logs --tail=300 worker > /tmp/nms_worker_fail.log
   ```
3. Сделать аварийный backup БД:
   ```bash
   ./scripts/backup_postgres.sh
   ```

## 3) Откат приложения (без отката БД)

Использовать предыдущий стабильный git-коммит/тег:

```bash
git checkout <stable_commit_or_tag>
docker compose up -d --build api worker trap-receiver
```

Проверить:

```bash
make smoke-test
make rbac-smoke
```

Если smoke зелёный — зафиксировать rollback done.

## 4) Откат БД (только при необходимости)

⚠️ Делать только если новый релиз повредил данные/схему и без rollback DB сервис не работает.

1. Выбрать нужный backup `.dump` из `backups/postgres/`.
2. Восстановить:
   ```bash
   ./scripts/restore_postgres.sh ./backups/postgres/<file.dump> NMS
   ```
3. Перезапустить сервисы:
   ```bash
   docker compose up -d --build api worker trap-receiver
   ```
4. Проверить smoke:
   ```bash
   make smoke-test
   make rbac-smoke
   ```

## 5) Политика миграций

- Если миграция применена и не имеет безопасного down-path, предпочтительнее:
  - rollback приложения на совместимую версию;
  - DB restore из backup при критической несовместимости.
- Не выполнять ручные SQL hotfix в проде без фиксации в миграциях.

## 6) Завершение инцидента

После стабилизации:
- задокументировать root cause;
- открыть задачу на исправление;
- обновить `PROD_CHECKLIST.md`/runbook при необходимости.

