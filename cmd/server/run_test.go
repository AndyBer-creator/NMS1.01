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
	"sync/atomic"
	"strings"
	"testing"
	"time"

	"NMS1/internal/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

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

func TestIsPublicBindAddress(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"", false},
		{"127.0.0.1:9000", false},
		{":9000", true},
		{"0.0.0.0:9000", true},
		{"[::]:9000", true},
		{"localhost", false},
	}
	for _, tc := range tests {
		if got := isPublicBindAddress(tc.addr); got != tc.want {
			t.Fatalf("isPublicBindAddress(%q)=%v want %v", tc.addr, got, tc.want)
		}
	}
}

func TestEnvIntOrDefault(t *testing.T) {
	t.Setenv("NMS_TEST_INT", "")
	if got := envIntOrDefault("NMS_TEST_INT", 7); got != 7 {
		t.Fatalf("empty env: got %d", got)
	}
	t.Setenv("NMS_TEST_INT", "11")
	if got := envIntOrDefault("NMS_TEST_INT", 7); got != 11 {
		t.Fatalf("valid env: got %d", got)
	}
	t.Setenv("NMS_TEST_INT", "bad")
	if got := envIntOrDefault("NMS_TEST_INT", 7); got != 7 {
		t.Fatalf("invalid env: got %d", got)
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	t.Setenv("NMS_TEST_BOOL", "")
	if got := envBoolOrDefault("NMS_TEST_BOOL", true); !got {
		t.Fatalf("empty env should use fallback")
	}
	t.Setenv("NMS_TEST_BOOL", "false")
	if got := envBoolOrDefault("NMS_TEST_BOOL", true); got {
		t.Fatalf("valid bool env should override fallback")
	}
	t.Setenv("NMS_TEST_BOOL", "oops")
	if got := envBoolOrDefault("NMS_TEST_BOOL", false); got {
		t.Fatalf("invalid bool env should use fallback")
	}
}

func TestFirstMetadataValue(t *testing.T) {
	if got := firstMetadataValue(nil); got != "" {
		t.Fatalf("nil slice: got %q", got)
	}
	if got := firstMetadataValue([]string{"a", "b"}); got != "a" {
		t.Fatalf("first value mismatch: %q", got)
	}
}

func TestGRPCTokenInterceptor(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/nms.Trap/Ingest"}

	interceptorNoToken := grpcTokenInterceptor(func() string { return "" })
	resp, err := interceptorNoToken(context.Background(), nil, info, handler)
	if status.Code(err) != codes.Unauthenticated || resp != nil {
		t.Fatalf("no token path should fail closed: resp=%v err=%v", resp, err)
	}

	interceptor := grpcTokenInterceptor(func() string { return "secret-token" })
	_, err = interceptor(context.Background(), nil, info, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing metadata: want unauthenticated, got %v", err)
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret-token"))
	resp, err = interceptor(ctx, nil, info, handler)
	if err != nil || resp != "ok" {
		t.Fatalf("bearer token path failed: resp=%v err=%v", resp, err)
	}

	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-nms-grpc-token", "secret-token"))
	resp, err = interceptor(ctx, nil, info, handler)
	if err != nil || resp != "ok" {
		t.Fatalf("x-nms-grpc-token path failed: resp=%v err=%v", resp, err)
	}
}

func TestNewCachedTokenProvider(t *testing.T) {
	var calls int32
	provider := newCachedTokenProvider(100*time.Millisecond, func() string {
		atomic.AddInt32(&calls, 1)
		return "token"
	})
	if got := provider(); got != "token" {
		t.Fatalf("unexpected token: %q", got)
	}
	_ = provider()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected single load call in ttl window, got %d", calls)
	}
	time.Sleep(120 * time.Millisecond)
	_ = provider()
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected reload after ttl, got %d", calls)
	}
}

func TestGRPCServerTLSCredsFromEnv(t *testing.T) {
	t.Setenv("NMS_GRPC_TLS_CERT_FILE", "")
	t.Setenv("NMS_GRPC_TLS_KEY_FILE", "")
	creds, err := grpcServerTLSCredsFromEnv()
	if err != nil || creds != nil {
		t.Fatalf("empty tls env should disable tls: creds=%v err=%v", creds, err)
	}

	t.Setenv("NMS_GRPC_TLS_CERT_FILE", "/no/such/cert.pem")
	t.Setenv("NMS_GRPC_TLS_KEY_FILE", "/no/such/key.pem")
	creds, err = grpcServerTLSCredsFromEnv()
	if err == nil || creds != nil {
		t.Fatalf("invalid cert/key should return error")
	}
}

func TestTrapIncidentSuppressionWindow(t *testing.T) {
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "")
	if got := trapIncidentSuppressionWindow(); got != 10*time.Minute {
		t.Fatalf("default window mismatch: %v", got)
	}
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "30s")
	if got := trapIncidentSuppressionWindow(); got != 30*time.Second {
		t.Fatalf("configured window mismatch: %v", got)
	}
	t.Setenv("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW", "bad")
	if got := trapIncidentSuppressionWindow(); got != 10*time.Minute {
		t.Fatalf("invalid window should fallback, got %v", got)
	}
}
