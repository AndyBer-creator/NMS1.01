package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"NMS1/internal/domain"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeleteByIDWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if err := repo.deleteByIDWithExec(ctx, db, 0); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for invalid id, got %v", err)
	}

	mock.ExpectExec(`DELETE FROM devices WHERE id = \$1`).
		WithArgs(5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.deleteByIDWithExec(ctx, db, 5); err != nil {
		t.Fatalf("deleteByIDWithExec success: %v", err)
	}

	mock.ExpectExec(`DELETE FROM devices WHERE id = \$1`).
		WithArgs(6).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.deleteByIDWithExec(ctx, db, 6); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows when no rows affected, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestGetDeviceByIDWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db, protector: &secretProtector{enabled: false}}
	ctx := context.Background()

	if got, err := repo.getDeviceByIDWithExec(ctx, db, 0); err != nil || got != nil {
		t.Fatalf("id<=0 should return nil,nil: got=%v err=%v", got, err)
	}

	mock.ExpectQuery(`FROM devices WHERE id = \$1`).
		WithArgs(10).
		WillReturnError(sql.ErrNoRows)
	if got, err := repo.getDeviceByIDWithExec(ctx, db, 10); err != nil || got != nil {
		t.Fatalf("no rows should return nil,nil: got=%v err=%v", got, err)
	}

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM devices WHERE id = \$1`).
		WithArgs(11).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ip", "name", "community", "community_enc", "version", "snmp_version",
			"auth_proto", "auth_pass", "auth_pass_enc", "priv_proto", "priv_pass", "priv_pass_enc",
			"status", "created_at", "last_seen", "last_error", "last_error_at", "last_poll_ok_at",
		}).AddRow(
			11, "10.0.0.1", "r1", "public", nil, "unknown", "v2c",
			"", "", nil, "", "", nil,
			"active", now, now, "", nil, nil,
		))
	got, err := repo.getDeviceByIDWithExec(ctx, db, 11)
	if err != nil {
		t.Fatalf("getDeviceByIDWithExec success: %v", err)
	}
	if got == nil || got.ID != 11 || got.IP != "10.0.0.1" || got.Community != "public" {
		t.Fatalf("unexpected device: %+v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestUpdateDeviceByIDWithExec_Validation(t *testing.T) {
	t.Parallel()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db, protector: &secretProtector{enabled: false}}
	ctx := context.Background()

	if _, err := repo.updateDeviceByIDWithExec(ctx, db, 0, &domain.Device{}); err == nil {
		t.Fatalf("expected error for missing id")
	}
	if _, err := repo.updateDeviceByIDWithExec(ctx, db, 1, nil); err == nil {
		t.Fatalf("expected error for nil patch")
	}
	if _, err := repo.updateDeviceByIDWithExec(ctx, db, 1, &domain.Device{SNMPVersion: "bad"}); err == nil {
		t.Fatalf("expected error for invalid snmp version")
	}
}

func TestDeviceStatusUpdateMethods(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectExec(`UPDATE devices SET last_seen = NOW\(\) WHERE id = \$1`).
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateDeviceLastSeen(ctx, 1); err != nil {
		t.Fatalf("UpdateDeviceLastSeen: %v", err)
	}

	mock.ExpectExec(`UPDATE devices SET status = \$1, last_seen = NOW\(\) WHERE id = \$2`).
		WithArgs("active", 2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateDeviceStatus(ctx, 2, "active"); err != nil {
		t.Fatalf("UpdateDeviceStatus: %v", err)
	}

	mock.ExpectExec(`UPDATE devices`).
		WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.MarkDevicePollSuccess(ctx, 3); err != nil {
		t.Fatalf("MarkDevicePollSuccess: %v", err)
	}

	mock.ExpectExec(`UPDATE devices`).
		WithArgs("failed", "boom", 4).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateDeviceError(ctx, 4, "", "  boom "); err != nil {
		t.Fatalf("UpdateDeviceError: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

