package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMIBSearchDirs_UploadsAndPublicVendor(t *testing.T) {
	cfg := &Config{}
	cfg.Paths.MibUploadDir = filepath.Join("tmp", "mibs", "uploads")
	got := MIBSearchDirs(cfg)
	if len(got) < 3 {
		t.Fatalf("expected at least 3 dirs, got %d: %v", len(got), got)
	}
	if got[0] != filepath.Clean(cfg.Paths.MibUploadDir) {
		t.Fatalf("first dir: got %q", got[0])
	}
	base := filepath.Dir(cfg.Paths.MibUploadDir)
	wantPub := filepath.Join(base, "public")
	wantVen := filepath.Join(base, "vendor")
	if got[len(got)-2] != wantPub || got[len(got)-1] != wantVen {
		t.Fatalf("expected public/vendor under mib base, got %#v", got)
	}
}

func TestMIBSearchDirs_CustomSearchDirsNoPublicVendor(t *testing.T) {
	cfg := &Config{}
	cfg.Paths.MibUploadDir = "/data/mibs/uploads"
	cfg.Paths.MibSearchDirs = []string{"/opt/mibs", "  ", "/data/mibs/uploads"}
	got := MIBSearchDirs(cfg)
	if len(got) != 2 {
		t.Fatalf("want 2 unique dirs, got %d: %v", len(got), got)
	}
	if got[0] != filepath.Clean("/data/mibs/uploads") || got[1] != filepath.Clean("/opt/mibs") {
		t.Fatalf("unexpected order/content: %#v", got)
	}
}

func TestMIBSearchDirs_Dedupe(t *testing.T) {
	cfg := &Config{}
	cfg.Paths.MibUploadDir = "/same"
	cfg.Paths.MibSearchDirs = []string{"/same", "/same"}
	got := MIBSearchDirs(cfg)
	if len(got) != 1 || got[0] != "/same" {
		t.Fatalf("dedupe failed: %#v", got)
	}
}

func TestSNMPClientConfig(t *testing.T) {
	var cfg Config
	cfg.SNMP.Port = 1161
	cfg.SNMP.Timeout = 7
	cfg.SNMP.Retries = 3
	port, timeout, retries := cfg.SNMPClientConfig()
	if port != 1161 || timeout != 7*time.Second || retries != 3 {
		t.Fatalf("got port=%d timeout=%v retries=%d", port, timeout, retries)
	}
}

func TestEnvDurationOrDefault(t *testing.T) {
	t.Setenv("NMS_TEST_DURATION", "")
	if got := EnvDurationOrDefault("NMS_TEST_DURATION", 5*time.Second); got != 5*time.Second {
		t.Fatalf("empty env should use fallback, got %v", got)
	}
	t.Setenv("NMS_TEST_DURATION", "1500ms")
	if got := EnvDurationOrDefault("NMS_TEST_DURATION", 5*time.Second); got != 1500*time.Millisecond {
		t.Fatalf("valid duration mismatch, got %v", got)
	}
	t.Setenv("NMS_TEST_DURATION", "not-a-duration")
	if got := EnvDurationOrDefault("NMS_TEST_DURATION", 5*time.Second); got != 5*time.Second {
		t.Fatalf("invalid duration should use fallback, got %v", got)
	}
	t.Setenv("NMS_TEST_DURATION", "-2s")
	if got := EnvDurationOrDefault("NMS_TEST_DURATION", 5*time.Second); got != 5*time.Second {
		t.Fatalf("non-positive duration should use fallback, got %v", got)
	}
}

func TestStandardOIDs_IncludesSysName(t *testing.T) {
	oids := StandardOIDs()
	if len(oids) == 0 {
		t.Fatal("expected non-empty OID list")
	}
	found := false
	for _, o := range oids {
		if strings.TrimSpace(o) == "1.3.6.1.2.1.1.5.0" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sysName OID missing: %#v", oids)
	}
}
