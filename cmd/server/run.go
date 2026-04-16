package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"NMS1/internal/config"
	h "NMS1/internal/delivery/http"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"
	"NMS1/internal/repository"
	"NMS1/internal/usecases/discovery"

	_ "github.com/jackc/pgx/v5/stdlib"

	"go.uber.org/zap"
)

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

// buildApp собирает HTTP-handler и cleanup (два отдельных *sql.DB: repo + traps).
func buildApp(cfg *config.Config, log *zap.Logger) (http.Handler, func(), error) {
	snmpClient := snmp.New(int(cfg.SNMP.Port),
		time.Duration(cfg.SNMP.Timeout)*time.Second, cfg.SNMP.Retries)

	repo, err := postgres.New(cfg.DB.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres repo: %w", err)
	}

	db, err := sql.Open("pgx", cfg.DB.DSN)
	if err != nil {
		_ = repo.Close()
		return nil, nil, fmt.Errorf("sql open: %w", err)
	}

	cleanup := func() {
		_ = repo.Close()
		_ = db.Close()
	}

	if err := os.MkdirAll(cfg.Paths.MibUploadDir, 0o755); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("mib upload dir: %w", err)
	}

	trapsRepo := repository.NewTrapsRepo(db)
	scanner := discovery.NewScanner(snmpClient, repo, log)
	mib := mibresolver.New(config.MIBSearchDirs(cfg), log)
	handlers := h.NewHandlers(repo, snmpClient, scanner, trapsRepo, log, cfg.Paths.MibUploadDir, mib)
	router := h.Router(handlers)
	return router, cleanup, nil
}

// run слушает TCP, обслуживает router до отмены ctx, затем graceful shutdown.
// onListen вызывается после успешного net.Listen (можно nil); для тестов — узнать ephemeral-порт.
func run(ctx context.Context, cfg *config.Config, log *zap.Logger, onListen func(net.Addr)) error {
	handler, cleanup, err := buildApp(cfg, log)
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
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
}
