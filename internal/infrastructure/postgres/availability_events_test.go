package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTruncateDetail(t *testing.T) {
	t.Parallel()

	short := truncateDetail("  ok  ")
	if short != "ok" {
		t.Fatalf("expected trimmed detail, got %q", short)
	}

	long := strings.Repeat("a", maxAvailabilityDetailLen+10)
	got := truncateDetail(long)
	if !strings.HasSuffix(got, "…") || len(got) <= maxAvailabilityDetailLen {
		t.Fatalf("expected truncated detail, got len=%d", len(got))
	}
}

func TestInsertAvailabilityEvent(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if err := repo.InsertAvailabilityEvent(ctx, 1, "unknown", "ignored"); err != nil {
		t.Fatalf("unexpected error for ignored kind: %v", err)
	}

	mock.ExpectExec(`INSERT INTO device_availability_events`).
		WithArgs(5, "available", "detail").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.InsertAvailabilityEvent(ctx, 5, " AVAILABLE ", " detail "); err != nil {
		t.Fatalf("InsertAvailabilityEvent: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListAvailabilityEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("list all uses default limit", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectQuery(`FROM device_availability_events e`).
			WithArgs(200).
			WillReturnRows(sqlmock.NewRows([]string{"id", "device_id", "ip", "name", "occurred_at", "kind", "detail"}).
				AddRow(1, 7, "10.0.0.7", "sw1", now, "available", "restored"))

		events, err := repo.ListAvailabilityEvents(ctx, 0, nil)
		if err != nil {
			t.Fatalf("ListAvailabilityEvents: %v", err)
		}
		if len(events) != 1 || events[0].DeviceID != 7 {
			t.Fatalf("unexpected events: %+v", events)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("filtered by device id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		deviceID := 9
		mock.ExpectQuery(`WHERE e.device_id = \$1`).
			WithArgs(deviceID, 50).
			WillReturnRows(sqlmock.NewRows([]string{"id", "device_id", "ip", "name", "occurred_at", "kind", "detail"}).
				AddRow(2, deviceID, "10.0.0.9", "sw9", now, "unavailable", "timeout"))

		events, err := repo.ListAvailabilityEvents(ctx, 50, &deviceID)
		if err != nil {
			t.Fatalf("ListAvailabilityEvents filtered: %v", err)
		}
		if len(events) != 1 || events[0].Kind != "unavailable" {
			t.Fatalf("unexpected events: %+v", events)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})
}
