package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoginRateLimitCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	window := 5 * time.Minute
	lockout := 10 * time.Minute

	t.Run("invalid input is allow by default", func(t *testing.T) {
		repo := &Repo{}
		allowed, wait, err := repo.LoginRateLimitCheck(ctx, "", "", now, window, lockout, 0)
		if err != nil || !allowed || wait != 0 {
			t.Fatalf("unexpected result: allowed=%v wait=%v err=%v", allowed, wait, err)
		}
	})

	t.Run("no rows means allowed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectExec(`DELETE FROM login_attempt_limits`).
			WithArgs(600).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT fail_count, first_fail_at, locked_until`).
			WithArgs("ip", "10.0.0.1").
			WillReturnError(sql.ErrNoRows)

		allowed, wait, err := repo.LoginRateLimitCheck(ctx, " ip ", "10.0.0.1", now, window, lockout, 3)
		if err != nil || !allowed || wait != 0 {
			t.Fatalf("unexpected result: allowed=%v wait=%v err=%v", allowed, wait, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("expired window resets record", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectExec(`DELETE FROM login_attempt_limits`).
			WithArgs(600).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT fail_count, first_fail_at, locked_until`).
			WithArgs("user", "admin").
			WillReturnRows(sqlmock.NewRows([]string{"fail_count", "first_fail_at", "locked_until"}).
				AddRow(2, now.Add(-10*time.Minute), nil))
		mock.ExpectExec(`UPDATE login_attempt_limits`).
			WithArgs("user", "admin", now).
			WillReturnResult(sqlmock.NewResult(0, 1))

		allowed, wait, err := repo.LoginRateLimitCheck(ctx, "user", "admin", now, window, lockout, 3)
		if err != nil || !allowed || wait != 0 {
			t.Fatalf("unexpected result: allowed=%v wait=%v err=%v", allowed, wait, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("active lock returns remaining duration", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		lockedUntil := now.Add(4 * time.Minute)
		mock.ExpectExec(`DELETE FROM login_attempt_limits`).
			WithArgs(600).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT fail_count, first_fail_at, locked_until`).
			WithArgs("user", "admin").
			WillReturnRows(sqlmock.NewRows([]string{"fail_count", "first_fail_at", "locked_until"}).
				AddRow(3, now.Add(-2*time.Minute), lockedUntil))

		allowed, wait, err := repo.LoginRateLimitCheck(ctx, "user", "admin", now, window, lockout, 3)
		if err != nil || allowed || wait != 4*time.Minute {
			t.Fatalf("unexpected result: allowed=%v wait=%v err=%v", allowed, wait, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("threshold reached sets lock", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectExec(`DELETE FROM login_attempt_limits`).
			WithArgs(600).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT fail_count, first_fail_at, locked_until`).
			WithArgs("user", "admin").
			WillReturnRows(sqlmock.NewRows([]string{"fail_count", "first_fail_at", "locked_until"}).
				AddRow(3, now.Add(-1*time.Minute), nil))
		mock.ExpectExec(`UPDATE login_attempt_limits`).
			WithArgs("user", "admin", now.Add(lockout), now).
			WillReturnResult(sqlmock.NewResult(0, 1))

		allowed, wait, err := repo.LoginRateLimitCheck(ctx, "user", "admin", now, window, lockout, 3)
		if err != nil || allowed || wait != lockout {
			t.Fatalf("unexpected result: allowed=%v wait=%v err=%v", allowed, wait, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})
}

func TestLoginRateLimitOnFailureAndSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	mock.ExpectExec(`INSERT INTO login_attempt_limits`).
		WithArgs("user", "admin", now, 300, 5, 600).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.LoginRateLimitOnFailure(ctx, " user ", "admin", now, 5*time.Minute, 10*time.Minute, 5); err != nil {
		t.Fatalf("LoginRateLimitOnFailure: %v", err)
	}

	mock.ExpectExec(`DELETE FROM login_attempt_limits`).
		WithArgs("10.0.0.1", "admin").
		WillReturnResult(sqlmock.NewResult(0, 2))
	if err := repo.LoginRateLimitOnSuccess(ctx, " 10.0.0.1 ", " admin "); err != nil {
		t.Fatalf("LoginRateLimitOnSuccess: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestLoginRateLimitOnFailureWrapsError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	mock.ExpectExec(`INSERT INTO login_attempt_limits`).
		WillReturnError(sql.ErrConnDone)

	err = repo.LoginRateLimitOnFailure(context.Background(), "user", "admin", time.Now().UTC(), time.Minute, time.Minute, 1)
	if err == nil || !strings.Contains(err.Error(), "login limiter failure upsert") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}
