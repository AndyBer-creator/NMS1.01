package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRecordPollFailureTransition(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(status, ''\) FROM devices WHERE id = \$1 FOR UPDATE`).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectExec(`INSERT INTO device_availability_events`).
		WithArgs(7, "unavailable", "failed: timeout").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE incidents i`).
		WithArgs([]byte(`{"source":"poller"}`), "polling", "SNMP device unavailable", "critical", sqlmock.AnyArg(), float64(300)).
		WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	err = repo.RecordPollFailureTransition(ctx, 7, "failed", "timeout", json.RawMessage(`{"source":"poller"}`), 5*time.Minute)
	if err == nil {
		t.Fatalf("expected incident touch/insert path error")
	}
}

func TestRecordPollFailureTransition_CreateIncident(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(status, ''\) FROM devices WHERE id = \$1 FOR UPDATE`).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectExec(`INSERT INTO device_availability_events`).
		WithArgs(7, "unavailable", "failed: timeout").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE incidents i`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO incidents`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "SNMP device unavailable", "critical", "polling", []byte(`{"source":"poller"}`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE devices`).
		WithArgs("failed", "timeout", 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = repo.RecordPollFailureTransition(ctx, 7, "failed", "timeout", json.RawMessage(`{"source":"poller"}`), 5*time.Minute)
	if err != nil {
		t.Fatalf("RecordPollFailureTransition: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRecordPollRecoveryTransition(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(status, ''\) FROM devices WHERE id = \$1 FOR UPDATE`).
		WithArgs(9).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("failed_timeout"))
	mock.ExpectExec(`INSERT INTO device_availability_events`).
		WithArgs(9, "available", "SNMP опрос восстановлен").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id, status`).
		WithArgs(9, "polling").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(42, "new"))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(42), "new", "system", "auto-resolved: SNMP poll restored").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE devices`).
		WithArgs(9).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = repo.RecordPollRecoveryTransition(ctx, 9)
	if err != nil {
		t.Fatalf("RecordPollRecoveryTransition: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRecordPollRecoveryTransition_NoIncidentWhenAlreadyActive(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(status, ''\) FROM devices WHERE id = \$1 FOR UPDATE`).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectExec(`UPDATE devices`).
		WithArgs(10).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = repo.RecordPollRecoveryTransition(ctx, 10)
	if err != nil {
		t.Fatalf("RecordPollRecoveryTransition active: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
