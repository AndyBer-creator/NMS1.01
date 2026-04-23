package main

import (
	"database/sql"
	"errors"

	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/timezone"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"
)

// main runs all database migrations and exits with fatal on failure.
func main() {
	timezone.InitFromEnv()
	logger := applog.MustNewZapFile("nms-migration")
	defer func() { _ = logger.Sync() }()

	dsn, err := migrationDSN()
	if err != nil {
		logger.Fatal("migration dsn", zap.Error(err))
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Fatal("migration db open", zap.Error(err))
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		logger.Fatal("migration dialect", zap.Error(err))
	}
	logger.Info("starting database migration")
	if err := goose.Up(db, "migrations"); err != nil {
		logger.Fatal("migration failed", zap.Error(err))
	}
	logger.Info("database migration completed")
}

// migrationDSN loads DB DSN from env/file and validates it is not empty.
func migrationDSN() (string, error) {
	dsn := config.EnvOrFile("DB_DSN")
	if dsn == "" {
		return "", errors.New("DB_DSN must be set (see .env)")
	}
	return dsn, nil
}
