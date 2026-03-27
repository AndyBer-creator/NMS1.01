package config

// StandardOIDs возвращает стандартный список OID'ов
func StandardOIDs() []string {
	return []string{
		// Система
		"1.3.6.1.2.1.1.1.0", // sysDescr
		"1.3.6.1.2.1.1.3.0", // sysUpTime
		"1.3.6.1.2.1.1.5.0", // sysName

		// Интерфейсы
		"1.3.6.1.2.1.2.1.0",      // ifNumber
		"1.3.6.1.2.1.2.2.1.10.1", // ifInOctets (первый интерфейс)
		"1.3.6.1.2.1.2.2.1.16.1", // ifOutOctets

		// CPU/Memory (Linux/Unix)
		"1.3.6.1.4.1.2021.10.1.3.1", // cpu 1min
		"1.3.6.1.4.1.2021.4.5.0",    // memTotal
		"1.3.6.1.4.1.2021.4.6.0",    // memAvail

		// MikroTik
		"1.3.6.1.4.1.14988.1.1.3.10.0", // CPU
		"1.3.6.1.4.1.14988.1.1.3.11.0", // Free HDD

		// Cisco
		"1.3.6.1.4.1.9.9.109.1.1.1.1.8.1", // CPU 5min
	}
}
