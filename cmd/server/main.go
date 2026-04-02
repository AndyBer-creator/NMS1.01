package main

import (
	"NMS1/internal/config"
	h "NMS1/internal/delivery/http" // АЛИАС 'h' для  пакета!
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/repository"
	"NMS1/internal/usecases/discovery"
	"context"
	"database/sql"
	"net/http" // стандартная библиотека
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	snmpClient := snmp.New(int(cfg.SNMP.Port),
		time.Duration(cfg.SNMP.Timeout)*time.Second, cfg.SNMP.Retries)

	repo, err := postgres.New(cfg.DB.DSN)
	if err != nil {
		logger.Fatal("DB failed", zap.Error(err))
	}
	defer repo.Close()

	logger.Info("DB connected OK")
	db, err := sql.Open("pgx", cfg.DB.DSN)
	if err != nil {
		logger.Fatal("DB direct failed", zap.Error(err))
	}

	trapsRepo := repository.NewTrapsRepo(db)
	logger.Info("TrapsRepo initialized")
	scanner := discovery.NewScanner(snmpClient, repo, logger)
	handlers := h.NewHandlers(repo, snmpClient, scanner, trapsRepo, logger)
	router := h.Router(handlers)

	srv := &http.Server{Addr: cfg.HTTP.Addr, Handler: router}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("Starting", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Listen failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Fatal("Shutdown failed", zap.Error(err))
	}
}
