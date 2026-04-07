package timezone

import (
	"log"
	"os"
	"strings"
	"time"

	_ "time/tzdata" // зона для статических бинарников без системной tzdata
)

// InitFromEnv выставляет time.Local по переменной TZ (например Europe/Moscow).
// Без TZ или при ошибке разбора зоны поведение не меняется.
func InitFromEnv() {
	tz := strings.TrimSpace(os.Getenv("TZ"))
	if tz == "" {
		return
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Printf("timezone: invalid TZ=%q: %v", tz, err)
		return
	}
	time.Local = loc
}
