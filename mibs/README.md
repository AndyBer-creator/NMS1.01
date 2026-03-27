## MIBs (public + private)

Эта папка предназначена для **public** и **vendor-private** MIB’ов и связанных mapping-файлов, чтобы UI мог предлагать “готовые операции” (port up/down, description, VLAN, PoE и т.д.) без ручного ввода OID.

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
- UI выбирает операцию → подставляет OID/тип/value и отправляет на `/devices/{ip}/snmp/set`

Стандартные операции уже реализованы в UI на базе IF-MIB:
- `ifAdminStatus` (\(1.3.6.1.2.1.2.2.1.7.{ifIndex}\)) — up/down
- `ifAlias` (\(1.3.6.1.2.1.31.1.1.1.18.{ifIndex}\)) — description

Дополнительно реализовано в UI:
- `Q-BRIDGE-MIB::dot1qPvid` (\(1.3.6.1.2.1.17.7.1.4.5.1.1.{ifIndex}\)) — access VLAN (PVID)
- `POWER-ETHERNET-MIB::pethPsePortAdminEnable` — PoE enable/disable (**индексация зависит от устройства**, обычно требуется vendor-профиль)

