package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func pollStatusWasOK(status string) bool {
	s := strings.TrimSpace(strings.ToLower(status))
	return s == "" || s == "active"
}

func pollStatusWasFailure(status string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(status)), "failed")
}

func insertAvailabilityEventTx(ctx context.Context, tx *sql.Tx, deviceID int, kind, detail string) error {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind != "unavailable" && kind != "available" {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO device_availability_events (device_id, kind, detail) VALUES ($1, $2, $3)`,
		deviceID, kind, truncateDetail(detail),
	)
	return err
}

func createOrTouchOpenIncidentTx(ctx context.Context, tx *sql.Tx, deviceID int, title, severity, source string, details json.RawMessage, suppressionWindow time.Duration) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title is required")
	}
	sv, err := normalizeIncidentSeverity(severity)
	if err != nil {
		return err
	}
	src, err := normalizeIncidentSource(source)
	if err != nil {
		return err
	}
	if suppressionWindow <= 0 {
		suppressionWindow = 10 * time.Minute
	}
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	windowSec := int(suppressionWindow.Seconds())
	if windowSec < 1 {
		windowSec = 1
	}
	devID := sql.NullInt64{Int64: int64(deviceID), Valid: deviceID > 0}

	var touchedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE incidents i
		   SET updated_at = NOW(),
		       details = $1::jsonb
		 WHERE i.id = (
		     SELECT id
		       FROM incidents
		      WHERE source = $2
		        AND status IN ('new', 'acknowledged', 'in_progress')
		        AND title = $3
		        AND severity = $4
		        AND (device_id IS NOT DISTINCT FROM $5)
		        AND updated_at >= NOW() - make_interval(secs => $6)
		      ORDER BY updated_at DESC
		      LIMIT 1
		 )
		RETURNING i.id`,
		[]byte(details), src, title, sv, devID, float64(windowSec),
	).Scan(&touchedID)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	var assignee sql.NullString
	if v := defaultIncidentAssignee(src, sv); v != nil && strings.TrimSpace(*v) != "" {
		assignee = sql.NullString{String: strings.TrimSpace(*v), Valid: true}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO incidents (device_id, assignee, title, severity, status, source, details)
		VALUES ($1, $2, $3, $4, 'new', $5, $6::jsonb)`,
		devID, assignee, title, sv, src, []byte(details),
	)
	return err
}

func resolveOpenIncidentsBySourceTx(ctx context.Context, tx *sql.Tx, deviceID int, source, changedBy, comment string) (int64, error) {
	if deviceID <= 0 {
		return 0, nil
	}
	src, err := normalizeIncidentSource(source)
	if err != nil {
		return 0, err
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "auto-resolved"
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, status
		  FROM incidents
		 WHERE device_id = $1
		   AND source = $2
		   AND status IN ('new', 'acknowledged', 'in_progress')
		 FOR UPDATE`, deviceID, src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	type incidentRow struct {
		id     int64
		status string
	}
	var items []incidentRow
	for rows.Next() {
		var it incidentRow
		if err := rows.Scan(&it.id, &it.status); err != nil {
			return 0, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var changed int64
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `
			UPDATE incidents
			   SET status = 'resolved',
			       updated_at = NOW(),
			       resolved_at = NOW()
			 WHERE id = $1`, it.id); err != nil {
			return changed, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
			VALUES ($1, $2, 'resolved', $3, $4)`,
			it.id, it.status, changedBy, comment,
		); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func (r *Repo) RecordPollFailureTransition(ctx context.Context, deviceID int, status, errText string, incidentDetails json.RawMessage, suppressionWindow time.Duration) error {
	if deviceID <= 0 {
		return fmt.Errorf("deviceID is required")
	}
	if strings.TrimSpace(status) == "" {
		status = "failed"
	}
	errText = strings.TrimSpace(errText)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var previousStatus string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(status, '') FROM devices WHERE id = $1 FOR UPDATE`, deviceID).Scan(&previousStatus); err != nil {
		return err
	}

	if pollStatusWasOK(previousStatus) {
		detail := status
		if errText != "" {
			detail += ": " + errText
		}
		if err := insertAvailabilityEventTx(ctx, tx, deviceID, "unavailable", detail); err != nil {
			return err
		}
		if err := createOrTouchOpenIncidentTx(ctx, tx, deviceID, "SNMP device unavailable", "critical", "polling", incidentDetails, suppressionWindow); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE devices
		   SET status = $1,
		       last_seen = NOW(),
		       last_error = $2,
		       last_error_at = NOW()
		 WHERE id = $3`,
		status, errText, deviceID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repo) RecordPollRecoveryTransition(ctx context.Context, deviceID int) error {
	if deviceID <= 0 {
		return fmt.Errorf("deviceID is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var previousStatus string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(status, '') FROM devices WHERE id = $1 FOR UPDATE`, deviceID).Scan(&previousStatus); err != nil {
		return err
	}

	if pollStatusWasFailure(previousStatus) {
		if err := insertAvailabilityEventTx(ctx, tx, deviceID, "available", "SNMP опрос восстановлен"); err != nil {
			return err
		}
		if _, err := resolveOpenIncidentsBySourceTx(ctx, tx, deviceID, "polling", "system", "auto-resolved: SNMP poll restored"); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE devices
		   SET status = 'active',
		       last_seen = NOW(),
		       last_poll_ok_at = NOW(),
		       last_error = NULL,
		       last_error_at = NULL
		 WHERE id = $1`,
		deviceID,
	); err != nil {
		return err
	}

	return tx.Commit()
}
