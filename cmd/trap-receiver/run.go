package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/grpcapi"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/repository"

	"github.com/gosnmp/gosnmp"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"go.uber.org/zap"
)

// run слушает SNMP traps на udpPort до отмены ctx.
func run(ctx context.Context, log *zap.Logger, dsn string, grpcTarget string, udpPort uint16) error {
	dsn = strings.TrimSpace(dsn)
	grpcTarget = strings.TrimSpace(grpcTarget)
	useGRPC := grpcTarget != ""
	if !useGRPC && dsn == "" {
		return fmt.Errorf("DB_DSN is required when NMS_TRAP_GRPC_TARGET is not set")
	}

	var (
		repo   *repository.TrapsRepo
		db     *sql.DB
		client grpcapi.TrapServiceClient
		conn   *grpc.ClientConn
		err    error
	)

	if useGRPC {
		grpcCreds, err := grpcClientTransportCreds()
		if err != nil {
			return err
		}
		tokenProvider := newTrapGRPCTokenProvider(ctx, dsn, log)
		unaryInterceptor := grpcAuthUnaryClientInterceptor(tokenProvider)
		conn, err = grpc.NewClient(
			grpcTarget,
			grpc.WithTransportCredentials(grpcCreds),
			grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcapi.JSONCodec{})),
			grpc.WithUnaryInterceptor(unaryInterceptor),
		)
		if err != nil {
			return fmt.Errorf("grpc dial: %w", err)
		}
		defer func() { _ = conn.Close() }()
		client = grpcapi.NewTrapServiceClient(conn)
		log.Info("trap forwarding via gRPC enabled",
			zap.String("target", grpcTarget),
			zap.Bool("tls_enabled", !isInsecureCreds(grpcCreds)),
			zap.Bool("token_auth_enabled", strings.TrimSpace(tokenProvider()) != ""))
	} else {
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("db open: %w", err)
		}
		defer func() { _ = db.Close() }()

		pingCtx, pingCancel := context.WithTimeout(ctx, 15*time.Second)
		defer pingCancel()
		if err := db.PingContext(pingCtx); err != nil {
			return fmt.Errorf("db ping: %w", err)
		}
		repo = repository.NewTrapsRepo(db)
	}
	suppressionWindow := trapIncidentSuppressionWindow()

	tl := gosnmp.NewTrapListener()
	tl.Params = gosnmp.Default
	tl.Params.Port = udpPort
	tl.OnNewTrap = func(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
		trapOID := "unknown"
		vars := make(map[string]string)
		for _, v := range packet.Variables {
			val := fmt.Sprintf("%v", v.Value)
			vars[v.Name] = val
			if v.Name == ".1.3.6.1.6.3.1.1.4.1.0" || v.Name == "1.3.6.1.6.3.1.1.4.1.0" {
				if val != "" {
					trapOID = val
				}
			}
		}
		if trapOID == "unknown" && packet.Enterprise != "" {
			trapOID = packet.Enterprise
		}

		// #nosec G115 -- Timestamp is a monotonic tick counter; clamp to int64 to avoid overflow on conversion.
		uptime := int64(uint64(packet.Timestamp) & uint64(^uint64(0)>>1))
		if useGRPC {
			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := client.IngestTrap(callCtx, &grpcapi.TrapIngestRequest{
				DeviceIP: addr.IP.String(),
				OID:      trapOID,
				Uptime:   uptime,
				TrapVars: vars,
			})
			callCancel()
			if err != nil {
				log.Error("Failed to forward trap via gRPC",
					zap.String("from", addr.IP.String()),
					zap.String("oid", trapOID),
					zap.Error(err))
				return
			}
		} else {
			insertCtx, insertCancel := context.WithTimeout(ctx, 5*time.Second)
			err := repo.Insert(insertCtx, addr.IP.String(), trapOID, uptime, vars, false)
			insertCancel()
			if err != nil {
				log.Error("Failed to persist trap",
					zap.String("from", addr.IP.String()),
					zap.String("oid", trapOID),
					zap.Error(err))
				return
			}
			incidentCtx, incidentCancel := context.WithTimeout(ctx, 5*time.Second)
			if err := repo.CreateOrTouchOpenTrapIncident(incidentCtx, addr.IP.String(), trapOID, vars, suppressionWindow); err != nil {
				log.Warn("Failed to correlate trap into incident",
					zap.String("from", addr.IP.String()),
					zap.String("oid", trapOID),
					zap.Error(err))
			}
			incidentCancel()
		}

		if useGRPC {
			log.Info("SNMP trap forwarded",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Int64("uptime", uptime))
		} else {
			log.Info("SNMP trap persisted",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Int64("uptime", uptime))
		}
		raw, _ := json.Marshal(vars)
		log.Debug("SNMP trap vars", zap.ByteString("vars", raw))
	}

	addr := fmt.Sprintf("0.0.0.0:%d", udpPort)
	log.Info("Listening SNMP traps", zap.String("addr", addr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- tl.Listen(addr)
	}()

	select {
	case <-ctx.Done():
		tl.Close()
		<-errCh
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("trap listen: %w", err)
		}
		return nil
	}
}

func grpcAuthUnaryClientInterceptor(tokenProvider func() string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		trimmed := strings.TrimSpace(tokenProvider())
		if trimmed != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+trimmed)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

type cachedStringProvider struct {
	mu        sync.RWMutex
	value     string
	expiresAt time.Time
	ttl       time.Duration
	load      func() string
}

func newCachedStringProvider(ttl time.Duration, load func() string) func() string {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	p := &cachedStringProvider{ttl: ttl, load: load}
	return func() string {
		now := time.Now()
		p.mu.RLock()
		if now.Before(p.expiresAt) {
			v := p.value
			p.mu.RUnlock()
			return v
		}
		p.mu.RUnlock()

		p.mu.Lock()
		defer p.mu.Unlock()
		now = time.Now()
		if now.Before(p.expiresAt) {
			return p.value
		}
		p.value = strings.TrimSpace(p.load())
		p.expiresAt = now.Add(p.ttl)
		return p.value
	}
}

func newTrapGRPCTokenProvider(ctx context.Context, dsn string, log *zap.Logger) func() string {
	envToken := strings.TrimSpace(config.EnvOrFile("NMS_TRAP_GRPC_AUTH_TOKEN"))
	ttl := config.EnvDurationOrDefault("NMS_TRAP_GRPC_AUTH_TOKEN_CACHE_TTL", 2*time.Second)
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return newCachedStringProvider(ttl, func() string { return envToken })
	}
	repo, err := postgres.New(dsn)
	if err != nil {
		if log != nil {
			log.Warn("trap-receiver: cannot init settings repo for gRPC token, using env fallback", zap.Error(err))
		}
		return newCachedStringProvider(ttl, func() string { return envToken })
	}
	go func() {
		<-ctx.Done()
		_ = repo.Close()
	}()
	return newCachedStringProvider(ttl, func() string {
		dbToken, err := repo.GetSecretSetting(context.Background(), postgres.SettingKeyGRPCAuthTokenSecret)
		if err == nil && strings.TrimSpace(dbToken) != "" {
			return strings.TrimSpace(dbToken)
		}
		return envToken
	})
}

func grpcClientTransportCreds() (credentials.TransportCredentials, error) {
	caPath := strings.TrimSpace(os.Getenv("NMS_TRAP_GRPC_TLS_CA_FILE"))
	certPath := strings.TrimSpace(os.Getenv("NMS_TRAP_GRPC_TLS_CERT_FILE"))
	keyPath := strings.TrimSpace(os.Getenv("NMS_TRAP_GRPC_TLS_KEY_FILE"))
	serverName := strings.TrimSpace(os.Getenv("NMS_TRAP_GRPC_TLS_SERVER_NAME"))
	if caPath == "" && certPath == "" && keyPath == "" && serverName == "" {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{
		// Keep client policy aligned with server-side hardening.
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2"},
	}
	if caPath != "" {
		// #nosec -- path is supplied by deployment; production guardrails validate *_FILE paths as absolute readable files.
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read grpc ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse grpc ca")
		}
		tlsCfg.RootCAs = pool
	}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("both NMS_TRAP_GRPC_TLS_CERT_FILE and NMS_TRAP_GRPC_TLS_KEY_FILE are required for mTLS")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load grpc client cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	if serverName != "" {
		tlsCfg.ServerName = serverName
	}
	return credentials.NewTLS(tlsCfg), nil
}

func isInsecureCreds(creds credentials.TransportCredentials) bool {
	return strings.EqualFold(creds.Info().SecurityProtocol, "insecure")
}

// trapIncidentSuppressionWindow returns dedup window for trap incidents.
func trapIncidentSuppressionWindow() time.Duration {
	raw := strings.TrimSpace(config.EnvOrFile("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW"))
	if raw == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}
