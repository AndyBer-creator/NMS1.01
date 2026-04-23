package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"NMS1/internal/config"

	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	if err := chdirModuleRoot(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/server tests: chdir module root: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func chdirModuleRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if st, e := os.Stat(filepath.Join(dir, "go.mod")); e == nil && !st.IsDir() {
			return os.Chdir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("go.mod not found from starting dir %q", dir)
		}
		dir = parent
	}
}

func testServerConfig(t *testing.T, mibDir, dsn string) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.DB.DSN = dsn
	cfg.HTTP.Addr = "127.0.0.1:0"
	cfg.Paths.MibUploadDir = mibDir
	cfg.SNMP.Port = 161
	cfg.SNMP.Timeout = 1
	cfg.SNMP.Retries = 1
	return cfg
}

func junkDSN() string {
	return "host=127.0.0.1 port=59998 user=u password=p dbname=n sslmode=disable"
}

func TestBuildApp_Health(t *testing.T) {
	cfg := testServerConfig(t, t.TempDir(), junkDSN())
	h, _, _, cleanup, err := buildApp(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "OK" {
		t.Fatalf("health: %d %q", res.StatusCode, body)
	}

	res2, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res2.Body.Close() }()
	body2, _ := io.ReadAll(res2.Body)
	if res2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ready with unreachable DB: want 503, got %d %s", res2.StatusCode, body2)
	}
}

func TestRun_InvalidListenAddr(t *testing.T) {
	cfg := testServerConfig(t, t.TempDir(), junkDSN())
	cfg.HTTP.Addr = "127.0.0.1:99999"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := run(ctx, cfg, zap.NewNop(), nil)
	if err == nil {
		t.Fatal("expected listen error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "listen") {
		t.Fatalf("expected listen-related error, got %v", err)
	}
}

func TestRun_GracefulShutdownAfterHealth(t *testing.T) {
	cfg := testServerConfig(t, t.TempDir(), junkDSN())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan net.Addr, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, zap.NewNop(), func(a net.Addr) { addrCh <- a })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-errCh:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for listen")
	}

	url := fmt.Sprintf("http://%s/health", addr)
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		cancel()
		<-errCh
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "OK" {
		cancel()
		<-errCh
		t.Fatalf("health: %d %q", res.StatusCode, body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}
