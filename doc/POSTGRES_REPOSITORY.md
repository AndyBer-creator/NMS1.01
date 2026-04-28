# PostgreSQL Repository Notes

Краткая заметка фиксирует текущее состояние декомпозиции `internal/infrastructure/postgres` после cleanup-эпика.

## Цели

- убрать монолитный `repo.go` как место для всей persistence-логики;
- выровнять read/write контракты вокруг `context.Context`;
- сделать методы переиспользуемыми как с `*sql.DB`, так и с `*sql.Tx`;
- упростить unit-тестирование через `sqlmock` без обязательной integration БД.

## Принципы

- `Repo` хранит только общий lifecycle/pool state и shared helpers;
- доменные зоны вынесены в отдельные файлы (`device_repo.go`, `metrics_repo.go`, `audit_repo.go`, `incident_*`, `settings_*`, `lldp_repo.go` и т.д.);
- для DB/Tx-совместимости используется минимальный интерфейс `sqlExecutor`;
- transactional orchestration централизована в `Repo.InTx(...)`;
- публичные методы репозитория остаются тонкими обёртками над `...WithExec` вариантами.

## Контракт изменений

- новые read/write методы должны принимать `context.Context`;
- если логика должна работать и внутри транзакции, и вне её, добавляется `...WithExec`;
- бизнес-нормализация и policy (`normalize*`, default assignee, dedup keys) держатся отдельно от SQL-обвязки;
- integration-тесты проверяют реальные round-trip сценарии, а unit/sqlmock-тесты закрывают ветвления и ошибки SQL-path.

## Что это дало

- меньше связности между доменами persistence;
- предсказуемые transaction boundary;
- дешёвое покрытие unit-тестами для большинства SQL-path;
- более безопасные refactor/change points для incident/settings/device flows.
