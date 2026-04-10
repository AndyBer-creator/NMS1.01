// Package testdb — общая проверка доступности PostgreSQL для интеграционных тестов.
package testdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PingDBOrSkip вызывает PingContext; при ошибке — t.Skipf (как при недоступном host=postgres с хоста).
func PingDBOrSkip(t *testing.T, db *sql.DB, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("integration: postgres unreachable (%v)", err)
	}
}

// PingDSNOrSkip открывает соединение по DSN, ping и закрывает; пустой DSN или ошибка ping — Skip.
func PingDSNOrSkip(t *testing.T, dsn string, timeout time.Duration) {
	t.Helper()
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		t.Skip("integration: set DB_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	defer func() { _ = db.Close() }()
	PingDBOrSkip(t, db, timeout)
}
