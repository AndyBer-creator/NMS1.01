## MIBs (public + private)

Эта папка предназначена для **public** и **vendor-private** MIB’ов и связанных mapping-файлов, чтобы UI мог предлагать “готовые операции” (port up/down, description, VLAN, PoE и т.д.) без ручного ввода OID.

Файлы, загруженные через веб-дашборд (роль admin), сохраняются в **`mibs/uploads/`** (в контейнере: `/app/mibs/uploads`). Переменная окружения **`MIB_UPLOAD_DIR`** переопределяет каталог.

### Использование в работе NMS

Сервис **api** вызывает **`snmptranslate`** (пакет **net-snmp-tools** в Docker-образе) с **`MIBDIRS`** из каталогов: uploads, `mibs/public`, `mibs/vendor` (и при наличии — системные `/usr/share/snmp/mibs`). Таким образом в запросах можно указывать **символьные OID** (например `IF-MIB::sysDescr.0`), они переводятся в числовые перед SNMP GET/SET.

- **`POST /mibs/resolve`** — JSON `{"symbol":"MY-MIB::myNode.0"}` → `{"oid":"1.3.6....","symbol":"..."}`.
- **`GET /devices/{id}/metric/{oid}`** — `{id}` — id устройства в БД; в `{oid}` допускается числовой OID или символьное имя (в URL кодировать спецсимволы).
- **`POST /devices/{id}/snmp/set`** — поле `oid` в JSON может быть символьным или числовым.

Периодический опрос **worker** по-прежнему использует **числовые OID** из `internal/config/oids.go`.

### Структура (рекомендуемая)

- `mibs/public/` — стандартные MIB’ы (IF-MIB, SNMPv2-MIB, ...), опционально
- `mibs/vendor/<vendor>/` — private MIB’ы конкретного производителя
- `mibs/profiles/` — YAML/JSON профили с mapping’ами операций → OID/тип/значение

### Как подключать в Docker

Контейнер будет видеть MIB’ы по пути **`/app/mibs`**.

- Dev: храните MIB’ы в `./mibs` и подключайте volume в `docker-compose.yml`.
- Prod: можно копировать `mibs/` в образ (или тоже монтировать volume).

### Быстрый старт без MIB-парсера

Чтобы получать пользу сразу, не парся ASN.1 MIB’ы:

- заведите профили в `mibs/profiles/*.yaml` с “операциями”
- UI выбирает операцию → подставляет OID/тип/value и отправляет на `/devices/{id}/snmp/set`

Стандартные операции уже реализованы в UI на базе IF-MIB:
- `ifAdminStatus` (\(1.3.6.1.2.1.2.2.1.7.{ifIndex}\)) — up/down
- `ifAlias` (\(1.3.6.1.2.1.31.1.1.1.18.{ifIndex}\)) — description

Дополнительно реализовано в UI:
- `Q-BRIDGE-MIB::dot1qPvid` (\(1.3.6.1.2.1.17.7.1.4.5.1.1.{ifIndex}\)) — access VLAN (PVID)
- `POWER-ETHERNET-MIB::pethPsePortAdminEnable` — PoE enable/disable (**индексация зависит от устройства**, обычно требуется vendor-профиль)

