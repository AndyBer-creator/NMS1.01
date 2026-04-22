package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"NMS1/internal/testdb"

	"go.uber.org/zap"
)

func junkTrapDSN() string {
	return "host=127.0.0.1 port=59996 user=u password=p dbname=n sslmode=disable"
}

func TestRun_EmptyDSN(t *testing.T) {
	ctx := context.Background()
	err := run(ctx, zap.NewNop(), "  ", "", 4163)
	if err == nil || !strings.Contains(err.Error(), "DB_DSN") {
		t.Fatalf("expected DB_DSN error, got %v", err)
	}
}

func TestRun_PingFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := run(ctx, zap.NewNop(), junkTrapDSN(), "", 4163)
	if err == nil {
		t.Fatal("expected db ping error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ping") {
		t.Fatalf("expected ping-related error, got %v", err)
	}
}

func TestRun_GracefulShutdownWithDB(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	testdb.PingDSNOrSkip(t, dsn, 5*time.Second)

	const udpPort = uint16(54321)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, zap.NewNop(), dsn, "", udpPort)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil after cancel, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for run to exit")
	}
}
