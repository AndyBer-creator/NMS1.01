package main

import (
	"database/sql"
	"errors"
	"log"

	"NMS1/internal/config"
	"NMS1/internal/timezone"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	timezone.InitFromEnv()
	dsn, err := migrationDSN()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatal(err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		log.Fatal(err)
	}
}

func migrationDSN() (string, error) {
	dsn := config.EnvOrFile("DB_DSN")
	if dsn == "" {
		return "", errors.New("DB_DSN must be set (see .env)")
	}
	return dsn, nil
}
