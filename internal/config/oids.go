package config

// StandardOIDs — OID для периодического опроса worker’ом.
// Только MIB-II / IF-MIB (общие для коммутаторов). Ранее в один GET добавлялись OID
// Linux NET-SNMP, MikroTik, Cisco — на части коммутаторов это приводило к таймауту
// всего запроса (при этом snmpget по 6 «базовым» OID с хоста работал).
func StandardOIDs() []string {
	return []string{
		// Система
		"1.3.6.1.2.1.1.1.0", // sysDescr
		"1.3.6.1.2.1.1.3.0", // sysUpTime
		"1.3.6.1.2.1.1.5.0", // sysName

		// Интерфейсы (индекс .1 — первый интерфейс в ifTable; на большинстве L2 — ожидаемо)
		"1.3.6.1.2.1.2.1.0",      // ifNumber
		"1.3.6.1.2.1.2.2.1.10.1", // ifInOctets
		"1.3.6.1.2.1.2.2.1.16.1", // ifOutOctets
	}
}
