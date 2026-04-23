package config

// StandardOIDs returns baseline OIDs for periodic worker polling.
// Kept to common MIB-II/IF-MIB entries to avoid vendor-specific timeout issues.
func StandardOIDs() []string {
	return []string{
		// System
		//"1.3.6.1.2.1.1.1.0", // sysDescr
		// "1.3.6.1.2.1.1.3.0", // sysUpTime
		"1.3.6.1.2.1.1.5.0", // sysName

		// Interfaces (.1 is first ifTable index on typical L2 devices)
		// 	"1.3.6.1.2.1.2.1.0",      // ifNumber
		// 	"1.3.6.1.2.1.2.2.1.10.1", // ifInOctets
		// 	"1.3.6.1.2.1.2.2.1.16.1", // ifOutOctets
	}
}
