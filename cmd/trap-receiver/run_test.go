package main

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"strings"
	"testing"
	"time"

	"NMS1/internal/testdb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

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

func TestGRPCAuthUnaryClientInterceptor(t *testing.T) {
	interceptor := grpcAuthUnaryClientInterceptor(func() string { return "token-1" })
	invoked := false
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		invoked = true
		md, _ := metadata.FromOutgoingContext(ctx)
		got := strings.Join(md.Get("authorization"), ",")
		if !strings.Contains(got, "Bearer token-1") {
			return errors.New("authorization header is missing")
		}
		return nil
	}
	if err := interceptor(context.Background(), "/svc/method", nil, nil, nil, invoker); err != nil {
		t.Fatalf("interceptor failed: %v", err)
	}
	if !invoked {
		t.Fatal("invoker was not called")
	}
}

func TestNewCachedStringProvider(t *testing.T) {
	var calls int32
	provider := newCachedStringProvider(80*time.Millisecond, func() string {
		atomic.AddInt32(&calls, 1)
		return "v1"
	})
	if got := provider(); got != "v1" {
		t.Fatalf("unexpected value: %q", got)
	}
	_ = provider()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one load in ttl window, got %d", calls)
	}
	time.Sleep(100 * time.Millisecond)
	_ = provider()
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected reload after ttl, got %d", calls)
	}
}

func TestNewTrapGRPCTokenProvider_EnvFallbackWithoutDSN(t *testing.T) {
	t.Setenv("NMS_TRAP_GRPC_AUTH_TOKEN", "  env-token  ")
	p := newTrapGRPCTokenProvider(context.Background(), "", zap.NewNop())
	if got := p(); got != "env-token" {
		t.Fatalf("unexpected token: %q", got)
	}
}

func TestGRPCClientTransportCreds(t *testing.T) {
	t.Setenv("NMS_TRAP_GRPC_TLS_CA_FILE", "")
	t.Setenv("NMS_TRAP_GRPC_TLS_CERT_FILE", "")
	t.Setenv("NMS_TRAP_GRPC_TLS_KEY_FILE", "")
	t.Setenv("NMS_TRAP_GRPC_TLS_SERVER_NAME", "")
	creds, err := grpcClientTransportCreds()
	if err != nil {
		t.Fatalf("unexpected error for insecure creds: %v", err)
	}
	if !isInsecureCreds(creds) {
		t.Fatalf("expected insecure creds, got %s", creds.Info().SecurityProtocol)
	}

	t.Setenv("NMS_TRAP_GRPC_TLS_CERT_FILE", "/no/such/cert.pem")
	t.Setenv("NMS_TRAP_GRPC_TLS_KEY_FILE", "")
	_, err = grpcClientTransportCreds()
	if err == nil {
		t.Fatal("expected error for partial mTLS config")
	}

	t.Setenv("NMS_TRAP_GRPC_TLS_CERT_FILE", "")
	t.Setenv("NMS_TRAP_GRPC_TLS_KEY_FILE", "")
	t.Setenv("NMS_TRAP_GRPC_TLS_CA_FILE", "/no/such/ca.pem")
	_, err = grpcClientTransportCreds()
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestIsInsecureCreds(t *testing.T) {
	if !isInsecureCreds(insecure.NewCredentials()) {
		t.Fatal("insecure creds should be detected as insecure")
	}
}

func TestTrapIncidentSuppressionWindow(t *testing.T) {
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "")
	if got := trapIncidentSuppressionWindow(); got != 10*time.Minute {
		t.Fatalf("default window mismatch: %v", got)
	}
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "45s")
	if got := trapIncidentSuppressionWindow(); got != 45*time.Second {
		t.Fatalf("configured window mismatch: %v", got)
	}
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "bad")
	if got := trapIncidentSuppressionWindow(); got != 10*time.Minute {
		t.Fatalf("invalid value should fallback, got %v", got)
	}
}
