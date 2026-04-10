package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"NMS1/internal/config"

	"go.uber.org/zap"
)

func testWorkerConfig(dsn string) *config.Config {
	cfg := &config.Config{}
	cfg.DB.DSN = dsn
	cfg.SNMP.Port = 161
	cfg.SNMP.Timeout = 1
	cfg.SNMP.Retries = 1
	return cfg
}

func junkWorkerDSN() string {
	return "host=127.0.0.1 port=59997 user=u password=p dbname=n sslmode=disable"
}

func TestStartMetricsHTTPServer_Metrics(t *testing.T) {
	log := zap.NewNop()
	srv, addr, err := startMetricsHTTPServer("127.0.0.1:0", log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(ctx)
	}()

	url := fmt.Sprintf("http://%s/metrics", addr)
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	if len(b) < 10 {
		t.Fatalf("short body: %q", b)
	}
}

func TestRun_CancelBeforeFirstPoll(t *testing.T) {
	cfg := testWorkerConfig(junkWorkerDSN())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := run(ctx, cfg, zap.NewNop(), workerOpts{metricsAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("expected nil on shutdown, got %v", err)
	}
}

func TestRun_InvalidMetricsAddrStillRuns(t *testing.T) {
	cfg := testWorkerConfig(junkWorkerDSN())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := run(ctx, cfg, zap.NewNop(), workerOpts{metricsAddr: "127.0.0.1:99999"})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
