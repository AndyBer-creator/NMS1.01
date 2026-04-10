package timezone

import (
	"testing"
	"time"
)

func TestInitFromEnv_EmptyTZ_NoPanic(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	t.Setenv("TZ", "")
	InitFromEnv()
}

func TestInitFromEnv_UTC(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	t.Setenv("TZ", "UTC")
	InitFromEnv()
	if time.Local != time.UTC {
		t.Fatalf("expected Local==UTC, got %v", time.Local)
	}
}

func TestInitFromEnv_InvalidTZ_KeepsPrevious(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	t.Setenv("TZ", "NotA/RealZone")
	InitFromEnv()
	if time.Local != old {
		t.Fatalf("invalid TZ should not change time.Local")
	}
}
