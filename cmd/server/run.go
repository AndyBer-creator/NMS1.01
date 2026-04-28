package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"NMS1/internal/config"
	h "NMS1/internal/delivery/http"
	"NMS1/internal/grpcapi"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"
	"NMS1/internal/repository"
	"NMS1/internal/usecases/discovery"

	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"golang.org/x/sync/singleflight"

	"go.uber.org/zap"
)

// isPublicBindAddress returns true for wildcard/unscoped listen addresses.
func isPublicBindAddress(addr string) bool {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, ":") {
		return true
	}
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		return false
	}
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::", "[::]":
		return true
	default:
		return false
	}
}

// envIntOrDefault parses a positive integer from env or returns fallback.
func envIntOrDefault(name string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// envBoolOrDefault parses bool from env or returns fallback.
func envBoolOrDefault(name string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func grpcTokenInterceptor(tokenProvider func() string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		required := strings.TrimSpace(tokenProvider())
		if required == "" {
			return nil, status.Error(codes.Unauthenticated, "missing token configuration")
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		authz := strings.TrimSpace(firstMetadataValue(md.Get("authorization")))
		if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			authz = strings.TrimSpace(authz[7:])
		} else if authz == "" {
			authz = strings.TrimSpace(firstMetadataValue(md.Get("x-nms-grpc-token")))
		}
		if subtle.ConstantTimeCompare([]byte(authz), []byte(required)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(ctx, req)
	}
}

func grpcTokenStreamInterceptor(tokenProvider func() string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		required := strings.TrimSpace(tokenProvider())
		if required == "" {
			return status.Error(codes.Unauthenticated, "missing token configuration")
		}
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		authz := strings.TrimSpace(firstMetadataValue(md.Get("authorization")))
		if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			authz = strings.TrimSpace(authz[7:])
		} else if authz == "" {
			authz = strings.TrimSpace(firstMetadataValue(md.Get("x-nms-grpc-token")))
		}
		if subtle.ConstantTimeCompare([]byte(authz), []byte(required)) != 1 {
			return status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(srv, ss)
	}
}

type cachedTokenProvider struct {
	mu        sync.RWMutex
	value     string
	expiresAt time.Time
	ttl       time.Duration
	load      func() string
	sf        singleflight.Group
}

func newCachedTokenProvider(ttl time.Duration, load func() string) func() string {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	p := &cachedTokenProvider{ttl: ttl, load: load}
	return func() string {
		now := time.Now()
		p.mu.RLock()
		if now.Before(p.expiresAt) {
			v := p.value
			p.mu.RUnlock()
			return v
		}
		p.mu.RUnlock()
		v, _, _ := p.sf.Do("token", func() (interface{}, error) {
			now := time.Now()
			p.mu.RLock()
			if now.Before(p.expiresAt) {
				cached := p.value
				p.mu.RUnlock()
				return cached, nil
			}
			p.mu.RUnlock()
			loaded := strings.TrimSpace(p.load())
			p.mu.Lock()
			p.value = loaded
			p.expiresAt = now.Add(p.ttl)
			p.mu.Unlock()
			return loaded, nil
		})
		s, _ := v.(string)
		return s
	}
}

func firstMetadataValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func grpcServerTLSCredsFromEnv() (credentials.TransportCredentials, error) {
	certPath := strings.TrimSpace(os.Getenv("NMS_GRPC_TLS_CERT_FILE"))
	keyPath := strings.TrimSpace(os.Getenv("NMS_GRPC_TLS_KEY_FILE"))
	if certPath == "" || keyPath == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load grpc tls cert/key: %w", err)
	}
	tlsCfg := &tls.Config{
		// Harden gRPC transport to modern protocol only.
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
	}
	clientCAPath := strings.TrimSpace(os.Getenv("NMS_GRPC_TLS_CLIENT_CA_FILE"))
	if clientCAPath != "" {
		// #nosec -- path is supplied by deployment; production guardrails validate *_FILE paths as absolute readable files.
		caPEM, err := os.ReadFile(clientCAPath)
		if err != nil {
			return nil, fmt.Errorf("read grpc client ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse grpc client ca")
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(tlsCfg), nil
}

// buildApp builds HTTP handler and shared DB-backed dependencies.
func buildApp(cfg *config.Config, log *zap.Logger) (http.Handler, *postgres.Repo, *sql.DB, func(), error) {
	snmpClient := snmp.New(int(cfg.SNMP.Port),
		time.Duration(cfg.SNMP.Timeout)*time.Second, cfg.SNMP.Retries)

	db, err := sql.Open("pgx", cfg.DB.DSN)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("sql open: %w", err)
	}
	repo, err := postgres.NewFromDB(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, fmt.Errorf("postgres repo: %w", err)
	}

	cleanup := func() {
		_ = repo.Close()
		_ = db.Close()
	}

	if err := os.MkdirAll(cfg.Paths.MibUploadDir, 0o750); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("mib upload dir: %w", err)
	}

	trapsRepo := repository.NewTrapsRepo(db)
	scanner := discovery.NewScanner(snmpClient, repo, log)
	mib := mibresolver.New(config.MIBSearchDirs(cfg), log)
	handlers := h.NewHandlers(repo, snmpClient, scanner, trapsRepo, log, cfg.Paths.MibUploadDir, mib)
	router := h.Router(handlers)
	return router, repo, db, cleanup, nil
}

// run слушает TCP, обслуживает router до отмены ctx, затем graceful shutdown.
// onListen вызывается после успешного net.Listen (можно nil); для тестов — узнать ephemeral-порт.
func run(ctx context.Context, cfg *config.Config, log *zap.Logger, onListen func(net.Addr)) error {
	handler, repo, db, cleanup, err := buildApp(cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = ln.Close() }()

	if onListen != nil {
		onListen(ln.Addr())
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: config.EnvDurationOrDefault("NMS_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       config.EnvDurationOrDefault("NMS_HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      config.EnvDurationOrDefault("NMS_HTTP_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:       config.EnvDurationOrDefault("NMS_HTTP_IDLE_TIMEOUT", 60*time.Second),
		MaxHeaderBytes:    envIntOrDefault("NMS_HTTP_MAX_HEADER_BYTES", 1<<20), // 1 MiB
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	var (
		grpcErrCh chan error
		grpcSrv   *grpc.Server
	)
	if grpcAddr := strings.TrimSpace(os.Getenv("NMS_GRPC_ADDR")); grpcAddr != "" {
		if isPublicBindAddress(grpcAddr) && !envBoolOrDefault("NMS_GRPC_ALLOW_PUBLIC_BIND", false) {
			return fmt.Errorf("unsafe NMS_GRPC_ADDR=%q: bind to loopback/private interface or set NMS_GRPC_ALLOW_PUBLIC_BIND=true explicitly", grpcAddr)
		}
		grpcLn, gerr := net.Listen("tcp", grpcAddr)
		if gerr != nil {
			return fmt.Errorf("grpc listen: %w", gerr)
		}
		defer func() { _ = grpcLn.Close() }()

		trapRepo := repository.NewTrapsRepo(db)
		grpcOpts := []grpc.ServerOption{
			grpc.ForceServerCodec(grpcapi.JSONCodec{}),
		}
		tokenProvider := newCachedTokenProvider(
			config.EnvDurationOrDefault("NMS_GRPC_AUTH_TOKEN_CACHE_TTL", 2*time.Second),
			func() string {
				t, err := repo.GetSecretSetting(context.Background(), postgres.SettingKeyGRPCAuthTokenSecret)
				if err == nil && strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
				return strings.TrimSpace(config.EnvOrFile("NMS_GRPC_AUTH_TOKEN"))
			},
		)
		allowInsecureIngest := envBoolOrDefault("NMS_GRPC_ALLOW_INSECURE_INGEST", false)
		requiredToken := strings.TrimSpace(tokenProvider())
		if requiredToken == "" && !allowInsecureIngest {
			return fmt.Errorf("grpc ingest token is empty: set DB key %q or NMS_GRPC_AUTH_TOKEN, or explicitly set NMS_GRPC_ALLOW_INSECURE_INGEST=true", postgres.SettingKeyGRPCAuthTokenSecret)
		}
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(grpcTokenInterceptor(tokenProvider)))
		grpcOpts = append(grpcOpts, grpc.StreamInterceptor(grpcTokenStreamInterceptor(tokenProvider)))
		tlsCreds, tlsErr := grpcServerTLSCredsFromEnv()
		if tlsErr != nil {
			return fmt.Errorf("grpc tls: %w", tlsErr)
		}
		if tlsCreds == nil && !allowInsecureIngest {
			return fmt.Errorf("grpc ingest tls is disabled: configure NMS_GRPC_TLS_CERT_FILE/NMS_GRPC_TLS_KEY_FILE or explicitly set NMS_GRPC_ALLOW_INSECURE_INGEST=true")
		}
		if tlsCreds != nil {
			grpcOpts = append(grpcOpts, grpc.Creds(tlsCreds))
		}
		grpcSrv = grpc.NewServer(grpcOpts...)
		grpcapi.RegisterTrapService(grpcSrv, &trapIngestService{
			repo:              trapRepo,
			log:               log,
			suppressionWindow: trapIncidentSuppressionWindow(),
		})
		grpcErrCh = make(chan error, 1)
		go func() {
			grpcErrCh <- grpcSrv.Serve(grpcLn)
		}()
		log.Info("gRPC trap ingest enabled",
			zap.String("addr", grpcAddr),
			zap.Bool("tls_enabled", tlsCreds != nil),
			zap.Bool("token_auth_enabled", requiredToken != ""),
			zap.Bool("insecure_ingest_override", allowInsecureIngest))
	}

	select {
	case <-ctx.Done():
		log.Info("Shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("after shutdown: %w", err)
		}
		if grpcSrv != nil {
			grpcSrv.GracefulStop()
		}
		if grpcErrCh != nil {
			if gerr := <-grpcErrCh; gerr != nil {
				return fmt.Errorf("grpc after shutdown: %w", gerr)
			}
		}
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case gerr := <-grpcErrCh:
		if gerr != nil {
			return fmt.Errorf("grpc serve: %w", gerr)
		}
		return nil
	}
}
