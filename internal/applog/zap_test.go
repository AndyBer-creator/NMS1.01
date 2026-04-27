package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestResolveLogDir(t *testing.T) {
	cases := []struct {
		name   string
		logDir string
		nmsEnv string
		want   string
	}{
		{name: "NMS_LOG_DIR wins", logDir: "/tmp/x", nmsEnv: "docker", want: "/tmp/x"},
		{name: "docker default", logDir: "", nmsEnv: "docker", want: "/app/logs"},
		{name: "local default", logDir: "", nmsEnv: "", want: "./logs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NMS_LOG_DIR", tc.logDir)
			t.Setenv("NMS_ENV", tc.nmsEnv)
			if got := ResolveLogDir(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNewZapFileWritesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NMS_LOG_DIR", dir)
	t.Setenv("NMS_ENV", "")

	log, err := NewZapFile("applog-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Sync() }()

	log.Info("hello from test")

	path := filepath.Join(dir, "applog-test.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hello from test") {
		t.Fatalf("log file missing message: %q", string(b))
	}
}

func TestResolveLogLevel(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		level zapcore.Level
	}{
		{name: "default info", raw: "", level: zapcore.InfoLevel},
		{name: "debug", raw: "debug", level: zapcore.DebugLevel},
		{name: "warn alias", raw: "warning", level: zapcore.WarnLevel},
		{name: "invalid fallback", raw: "verbose", level: zapcore.InfoLevel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NMS_LOG_LEVEL", tc.raw)
			if got := resolveLogLevel(); got != tc.level {
				t.Fatalf("resolveLogLevel(%q)=%v want %v", tc.raw, got, tc.level)
			}
		})
	}
}
