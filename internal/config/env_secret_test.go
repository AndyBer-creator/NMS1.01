package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvOrFile_PrefersFileValue(t *testing.T) {
	key := "TEST_SECRET_PREFERS_FILE"
	_ = os.Unsetenv(key)
	_ = os.Unsetenv(key + "_FILE")
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(key + "_FILE")
	})

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	if err := os.Setenv(key, "env-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv(key+"_FILE", secretPath); err != nil {
		t.Fatalf("set env file: %v", err)
	}

	got := EnvOrFile(key)
	if got != "file-secret" {
		t.Fatalf("expected file-secret, got %q", got)
	}
}

func TestEnvOrFile_FallbackToEnvWhenFileMissing(t *testing.T) {
	key := "TEST_SECRET_FILE_MISSING"
	_ = os.Unsetenv(key)
	_ = os.Unsetenv(key + "_FILE")
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(key + "_FILE")
	})

	if err := os.Setenv(key, "env-only-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv(key+"_FILE", "/path/does/not/exist"); err != nil {
		t.Fatalf("set env file: %v", err)
	}

	got := EnvOrFile(key)
	if got != "env-only-secret" {
		t.Fatalf("expected env-only-secret, got %q", got)
	}
}

func TestEnvOrFile_TrimAndEmptyRules(t *testing.T) {
	key := "TEST_SECRET_TRIM"
	_ = os.Unsetenv(key)
	_ = os.Unsetenv(key + "_FILE")
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(key + "_FILE")
	})

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(secretPath, []byte("   \n"), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	if err := os.Setenv(key, "  env-trim  "); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv(key+"_FILE", secretPath); err != nil {
		t.Fatalf("set env file: %v", err)
	}

	got := EnvOrFile(key)
	if got != "env-trim" {
		t.Fatalf("expected env-trim, got %q", got)
	}
}

