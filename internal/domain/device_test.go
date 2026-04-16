package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDevice_JSONOmitsSecrets(t *testing.T) {
	d := Device{
		ID:          1,
		IP:          "10.0.0.1",
		Name:        "sw1",
		Community:   "public",
		AuthPass:    "secret-auth",
		PrivPass:    "secret-priv",
		CreatedAt:   time.Unix(1, 0).UTC(),
		LastSeen:    time.Unix(2, 0).UTC(),
		LastErrorAt: time.Unix(0, 0).UTC(),
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "secret-auth") || strings.Contains(s, "secret-priv") || strings.Contains(s, "public") {
		t.Fatalf("JSON must not contain passwords: %s", s)
	}
	if !strings.Contains(s, "sw1") || !strings.Contains(s, "10.0.0.1") {
		t.Fatalf("JSON should include non-secret fields: %s", s)
	}
}
