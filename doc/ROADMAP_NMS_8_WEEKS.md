# Roadmap: NMS на 8 недель

Дата фиксации: 2026-04-15

## Цели этапа

- Перевести текущую систему от набора событий к операционной модели инцидентов.
- Снизить шум событий за счет дедупликации и корреляции.
- Открыть стабильный northbound API для интеграций (ITSM/SIEM/automation).
- Закрыть P0/P1 security-hardening из backlog.

## Sprint 1 (Week 1-2): Security + Foundation

### Deliverables

- Trusted-proxy boundary для `X-Forwarded-*` и `X-Real-IP` (done).
- Убрать token из query в terminal WebSocket (перенос в subprotocol/nonce).
- CI hardening: pin GitHub Actions по commit SHA, сузить `id-token` permissions.
- Coverage gate минимум 35% + минимальные пороги по критичным пакетам.

### Выходные критерии

- HTTPS/session/IP behavior не зависит от неподтвержденных forwarded headers.
- CI-пайплайн детерминирован и не использует mutable action tags.
- Основной e2e поток авторизации и RBAC стабильно проходит в CI.

## Sprint 2 (Week 3-4): Events -> Incidents

### Deliverables

- Новые сущности: `incidents`, `incident_events`, `incident_transitions`.
- Lifecycle: `new -> acknowledged -> in_progress -> resolved -> closed`.
- Базовые правила создания инцидентов из traps и availability events.
- UI-страница и API-эндпоинты для списка/карточки/смены статуса инцидента.

### Выходные критерии

- Каждый critical event либо привязан к существующему, либо создает новый инцидент.
- Есть аудит переходов по статусам и оператору.

## Sprint 3 (Week 5-6): Correlation + Noise Reduction

### Deliverables

- Дедупликация по окну времени (`suppression window`).
- Корреляционные правила (минимум 3): `linkDown + BFD down`, flapping, device unreachable.
- Severity mapping и нормализация trap payload в общий event model.
- Политики auto-resolve для recover-событий.

### Выходные критерии

- Снижение количества дублирующих инцидентов минимум на 40% на тестовом стенде.
- Повторные траппы в окне дедупликации не создают новые инциденты.

## Sprint 4 (Week 7-8): Integrations + Operations

### Deliverables

- Northbound API v1 для incidents/events с фильтрами и пагинацией.
- Каналы уведомлений: webhook + Telegram/Slack с маршрутизацией по severity.
- Эскалация: переход на следующий уровень поддержки по SLA-таймеру.
- Документация для интеграций и runbook инцидентов.

### Выходные критерии

- Инциденты могут создаваться/обновляться извне через API.
- Эскалация и уведомления воспроизводимо проходят сценарии smoke-test.

## Последовательность внедрения (dependency order)

1. Security-hardening (без этого рискованно расширять интеграции).
2. Incident model и lifecycle.
3. Correlation/dedup и шумоподавление.
4. Northbound API и внешние каналы.

## Риски

- Высокий шум trap-источников без дедупликации может перегружать операторов.
- Неконсистентный severity mapping между vendors.
- Изменения API без строгого контракт-контроля могут ломать интеграции.

## Метрики успеха

- MTTA/MTTR по инцидентам.
- Доля auto-correlated инцидентов.
- Количество дубликатов на 1000 событий.
- Доля инцидентов, обработанных в SLA.
