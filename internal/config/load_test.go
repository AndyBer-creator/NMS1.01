package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoad_SetsDefaultsFromEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	t.Setenv("DB_DSN", "postgres://localhost/nms_test")
	t.Setenv("NMS_ENV", "")
	t.Setenv("MIB_UPLOAD_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DB.DSN != "postgres://localhost/nms_test" {
		t.Fatalf("DB.DSN: got %q", cfg.DB.DSN)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP.Addr: got %q", cfg.HTTP.Addr)
	}
	if cfg.SNMP.Port != 161 || cfg.SNMP.Timeout != 3 || cfg.SNMP.Retries != 1 {
		t.Fatalf("SNMP defaults: %+v", cfg.SNMP)
	}
	wantUpload := filepath.Join("mibs", "uploads")
	if cfg.Paths.MibUploadDir != wantUpload {
		t.Fatalf("MibUploadDir: got %q want %q", cfg.Paths.MibUploadDir, wantUpload)
	}
}

func TestLoad_DockerMibUploadDir(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	t.Setenv("DB_DSN", "postgres://localhost/nms_test")
	t.Setenv("NMS_ENV", "docker")
	t.Setenv("MIB_UPLOAD_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths.MibUploadDir != "/app/mibs/uploads" {
		t.Fatalf("MibUploadDir: got %q", cfg.Paths.MibUploadDir)
	}
}

func TestLoad_MibUploadDirOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	t.Setenv("DB_DSN", "postgres://localhost/nms_test")
	t.Setenv("MIB_UPLOAD_DIR", "/custom/mibs/up")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths.MibUploadDir != "/custom/mibs/up" {
		t.Fatalf("MibUploadDir: got %q", cfg.Paths.MibUploadDir)
	}
}

func TestLoad_InvalidConfigYAMLReturnsError(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
	t.Setenv("DB_DSN", "postgres://localhost/nms_test")

	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	if err := os.WriteFile("config.yaml", []byte("http: [invalid"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid config.yaml")
	}
}
