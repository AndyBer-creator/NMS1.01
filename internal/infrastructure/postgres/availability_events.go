package postgres

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const maxAvailabilityDetailLen = 8000

// AvailabilityEvent is one historical availability transition record.
type AvailabilityEvent struct {
	ID         int64     `json:"id"`
	DeviceID   int       `json:"device_id"`
	IP         string    `json:"ip"`
	Name       string    `json:"name"`
	OccurredAt time.Time `json:"occurred_at"`
	Kind       string    `json:"kind"` // unavailable | available
	Detail     string    `json:"detail,omitempty"`
}

func truncateDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxAvailabilityDetailLen {
		return s
	}
	return s[:maxAvailabilityDetailLen] + "…"
}

func (r *Repo) InsertAvailabilityEvent(ctx context.Context, deviceID int, kind, detail string) error {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind != "unavailable" && kind != "available" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO device_availability_events (device_id, kind, detail) VALUES ($1, $2, $3)`,
		deviceID, kind, truncateDetail(detail),
	)
	return err
}

func (r *Repo) ListAvailabilityEvents(ctx context.Context, limit int, deviceID *int) ([]AvailabilityEvent, error) {
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	var rows *sql.Rows
	var err error
	if deviceID != nil && *deviceID > 0 {
		rows, err = r.db.QueryContext(ctx, `
			SELECT e.id, e.device_id, d.ip::text, COALESCE(d.name, ''), e.occurred_at, e.kind, COALESCE(e.detail, '')
			FROM device_availability_events e
			JOIN devices d ON d.id = e.device_id
			WHERE e.device_id = $1
			ORDER BY e.occurred_at DESC
			LIMIT $2`, *deviceID, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT e.id, e.device_id, d.ip::text, COALESCE(d.name, ''), e.occurred_at, e.kind, COALESCE(e.detail, '')
			FROM device_availability_events e
			JOIN devices d ON d.id = e.device_id
			ORDER BY e.occurred_at DESC
			LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]AvailabilityEvent, 0)
	for rows.Next() {
		var ev AvailabilityEvent
		if err := rows.Scan(&ev.ID, &ev.DeviceID, &ev.IP, &ev.Name, &ev.OccurredAt, &ev.Kind, &ev.Detail); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
