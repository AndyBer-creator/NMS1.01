package main

import (
	"os"
	"strconv"
	"strings"
)

const defaultTrapUDPPort = uint16(162)

// trapListenPort — UDP-порт приёма трапов из TRAP_PORT (если задан и валиден).
func trapListenPort() uint16 {
	p := strings.TrimSpace(os.Getenv("TRAP_PORT"))
	if p == "" {
		return defaultTrapUDPPort
	}
	n, err := strconv.ParseUint(p, 10, 16)
	if err != nil {
		return defaultTrapUDPPort
	}
	return uint16(n)
}
