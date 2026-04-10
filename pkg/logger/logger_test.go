package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesToNMSLogDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NMS_LOG_DIR", dir)
	t.Setenv("NMS_ENV", "")

	l := New("pkglogger-test")
	l.Info("ping")

	path := filepath.Join(dir, "pkglogger-test.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ping") {
		t.Fatalf("expected log line with ping, got %q", string(b))
	}
}

func TestWithDeviceFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NMS_LOG_DIR", dir)
	t.Setenv("NMS_ENV", "")

	l := New("pkglogger-device")
	var buf bytes.Buffer
	entry := l.WithDevice("10.0.0.1", "core")
	entry.Logger.SetOutput(&buf)
	entry.Info("x")

	s := buf.String()
	if !strings.Contains(s, "10.0.0.1") || !strings.Contains(s, "core") {
		t.Fatalf("expected device fields in log: %q", s)
	}
}
