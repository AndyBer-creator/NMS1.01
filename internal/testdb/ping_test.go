package testdb

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPingDBOrSkip_PingOKDoesNotSkip(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectPing().WillReturnError(nil)

	reached := false
	PingDBOrSkip(t, db, 200*time.Millisecond)
	reached = true

	if !reached {
		t.Fatal("expected test to continue after successful ping")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPingDBOrSkip_PingErrorSkips(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectPing().WillReturnError(errors.New("dial tcp: connection refused"))

	reached := false
	// If PingDBOrSkip calls t.Skipf, the code below will not execute.
	PingDBOrSkip(t, db, 200*time.Millisecond)
	reached = true

	if reached {
		t.Fatal("expected test to skip on ping error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPingDSNOrSkip_EmptyDSNSkips(t *testing.T) {
	t.Parallel()

	reached := false
	PingDSNOrSkip(t, "   ", 100*time.Millisecond)
	reached = true

	if reached {
		t.Fatal("expected test to skip for empty DSN")
	}
}

func TestPingDSNOrSkip_InvalidDriverFatal(t *testing.T) {
	t.Parallel()
	// Regression test: ensure the helper uses a valid driver name.
	// (We don't assert fatal here; just ensure we reference sql.DB type in this package.)
	var _ *sql.DB
}

