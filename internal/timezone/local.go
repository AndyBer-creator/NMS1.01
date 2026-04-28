package timezone

import (
	"log"
	"os"
	"strings"
	"time"

	_ "time/tzdata" // include tzdata for static binaries without system zoneinfo
)

// InitFromEnv sets time.Local from TZ environment variable.
// No-op when TZ is empty or invalid.
func InitFromEnv() {
	tz := strings.TrimSpace(os.Getenv("TZ"))
	if tz == "" {
		return
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Printf("timezone: invalid TZ: %v", err)
		return
	}
	time.Local = loc
}
