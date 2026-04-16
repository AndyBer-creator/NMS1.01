package domain

import (
	"fmt"
	"strings"
)

// NormalizeSNMPVersion validates and normalizes supported SNMP versions.
func NormalizeSNMPVersion(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "v1":
		return "v1", nil
	case "v3":
		return "v3", nil
	case "2c", "v2c", "":
		return "v2c", nil
	default:
		return "", fmt.Errorf("invalid snmp_version %q (allowed: v1, v2c, v3)", v)
	}
}

// NormalizeSNMPVersionOrDefault keeps legacy behavior for permissive callers.
func NormalizeSNMPVersionOrDefault(v string) string {
	n, err := NormalizeSNMPVersion(v)
	if err != nil {
		return "v2c"
	}
	return n
}
