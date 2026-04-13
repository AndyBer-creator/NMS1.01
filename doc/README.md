# Документация NMS1

Операционные и процессные материалы. Точка входа в проект — [README.md](../README.md) в корне репозитория.

| Документ | Назначение |
|----------|------------|
| [API_MIGRATION.md](API_MIGRATION.md) | Переход с URL `/devices/<ip>/…` на `/devices/<id>/…` и трапы `?device_id=` |
| [ENTERPRISE.md](ENTERPRISE.md) | Целевой enterprise-уровень: `/ready`, OpenAPI, security.txt, корреляция запросов |
| [BACKUP_RESTORE.md](BACKUP_RESTORE.md) | Резервное копирование и восстановление PostgreSQL |
| [GO_LIVE_CHECKLIST.md](GO_LIVE_CHECKLIST.md) | Чеклист перед выводом в эксплуатацию |
| [PROD_CHECKLIST.md](PROD_CHECKLIST.md) | Статус production readiness |
| [ROLLBACK.md](ROLLBACK.md) | Откат релиза и БД |
| [RUNBOOK.md](RUNBOOK.md) | Дежурство и инциденты |
| [SECRETS_POLICY.md](SECRETS_POLICY.md) | Политика секретов |
| [SECRETS_PROCESS.md](SECRETS_PROCESS.md) | Процесс Docker-secrets |
| [SLO_GATES.md](SLO_GATES.md) | Пороги SLO и проверки |
