package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// required blank import for pgx stdlib driver registration
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Repo provides PostgreSQL-backed persistence for core NMS entities.
type Repo struct {
	db        *sql.DB
	protector *secretProtector
	ownDB     bool
}

// New opens PostgreSQL connections and initializes secret protection helpers.
func New(dsn string) (*Repo, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	applyDefaultPoolSettings(db)
	protector, err := newSecretProtectorFromEnv()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Repo{db: db, protector: protector, ownDB: true}, nil
}

// NewFromDB wires repo to an existing SQL connection pool.
func NewFromDB(db *sql.DB) (*Repo, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	applyDefaultPoolSettings(db)
	protector, err := newSecretProtectorFromEnv()
	if err != nil {
		return nil, err
	}
	return &Repo{db: db, protector: protector, ownDB: false}, nil
}

func (r *Repo) Close() error {
	if r == nil || r.db == nil || !r.ownDB {
		return nil
	}
	return r.db.Close()
}

func applyDefaultPoolSettings(db *sql.DB) {
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
}

// Ping checks database availability for readiness probes.
func (r *Repo) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}
