package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

var allowedIncidentTransitions = map[string]map[string]struct{}{
	"new": {
		"acknowledged": {},
	},
	"acknowledged": {
		"in_progress": {},
		"resolved":    {},
	},
	"in_progress": {
		"resolved": {},
	},
	"resolved": {
		"closed": {},
	},
}

func normalizeIncidentSeverity(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "critical":
		return "critical", nil
	case "info":
		return "info", nil
	case "", "warning":
		return "warning", nil
	default:
		return "", fmt.Errorf("invalid severity %q", v)
	}
}

func normalizeIncidentSource(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "polling":
		return "polling", nil
	case "trap":
		return "trap", nil
	case "", "manual":
		return "manual", nil
	default:
		return "", fmt.Errorf("invalid source %q", v)
	}
}

func normalizeIncidentStatus(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "new":
		return "new", nil
	case "acknowledged":
		return "acknowledged", nil
	case "in_progress":
		return "in_progress", nil
	case "resolved":
		return "resolved", nil
	case "closed":
		return "closed", nil
	default:
		return "", fmt.Errorf("invalid status %q", v)
	}
}

func (r *Repo) CreateIncident(input *domain.Incident) (*domain.Incident, error) {
	if input == nil {
		return nil, fmt.Errorf("incident input is required")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	severity, err := normalizeIncidentSeverity(input.Severity)
	if err != nil {
		return nil, err
	}
	source, err := normalizeIncidentSource(input.Source)
	if err != nil {
		return nil, err
	}
	details := input.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	var devID sql.NullInt64
	if input.DeviceID != nil && *input.DeviceID > 0 {
		devID = sql.NullInt64{Int64: int64(*input.DeviceID), Valid: true}
	}
	var out domain.Incident
	var outDevID sql.NullInt64
	var ackAt, resolvedAt, closedAt sql.NullTime
	if err := r.db.QueryRowContext(
		context.Background(),
		`INSERT INTO incidents (device_id, title, severity, status, source, details)
         VALUES ($1, $2, $3, 'new', $4, $5::jsonb)
         RETURNING id, device_id, title, severity, status, source, details,
                   created_at, updated_at, acknowledged_at, resolved_at, closed_at`,
		devID, title, severity, source, []byte(details),
	).Scan(
		&out.ID,
		&outDevID,
		&out.Title,
		&out.Severity,
		&out.Status,
		&out.Source,
		&out.Details,
		&out.CreatedAt,
		&out.UpdatedAt,
		&ackAt,
		&resolvedAt,
		&closedAt,
	); err != nil {
		return nil, err
	}
	if outDevID.Valid {
		v := int(outDevID.Int64)
		out.DeviceID = &v
	}
	return &out, nil
}

func (r *Repo) GetIncidentByID(id int64) (*domain.Incident, error) {
	if id <= 0 {
		return nil, nil
	}
	var out domain.Incident
	var devID sql.NullInt64
	var ackAt, resolvedAt, closedAt sql.NullTime
	err := r.db.QueryRowContext(
		context.Background(),
		`SELECT id, device_id, title, severity, status, source, details,
                created_at, updated_at, acknowledged_at, resolved_at, closed_at
         FROM incidents WHERE id = $1`,
		id,
	).Scan(
		&out.ID,
		&devID,
		&out.Title,
		&out.Severity,
		&out.Status,
		&out.Source,
		&out.Details,
		&out.CreatedAt,
		&out.UpdatedAt,
		&ackAt,
		&resolvedAt,
		&closedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if devID.Valid {
		v := int(devID.Int64)
		out.DeviceID = &v
	}
	if ackAt.Valid {
		out.AcknowledgedAt = &ackAt.Time
	}
	if resolvedAt.Valid {
		out.ResolvedAt = &resolvedAt.Time
	}
	if closedAt.Valid {
		out.ClosedAt = &closedAt.Time
	}
	return &out, nil
}

func (r *Repo) ListIncidents(limit int, deviceID *int, status, severity string) ([]domain.Incident, error) {
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	args := []any{}
	conds := []string{}
	if deviceID != nil && *deviceID > 0 {
		args = append(args, *deviceID)
		conds = append(conds, fmt.Sprintf("device_id = $%d", len(args)))
	}
	if strings.TrimSpace(status) != "" {
		s, err := normalizeIncidentStatus(status)
		if err != nil {
			return nil, err
		}
		args = append(args, s)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(severity) != "" {
		sv, err := normalizeIncidentSeverity(severity)
		if err != nil {
			return nil, err
		}
		args = append(args, sv)
		conds = append(conds, fmt.Sprintf("severity = $%d", len(args)))
	}
	args = append(args, limit)
	query := `SELECT id, device_id, title, severity, status, source, details,
                     created_at, updated_at, acknowledged_at, resolved_at, closed_at
              FROM incidents`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", len(args))
	rows, err := r.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.Incident, 0)
	for rows.Next() {
		var it domain.Incident
		var devID sql.NullInt64
		var ackAt, resolvedAt, closedAt sql.NullTime
		if err := rows.Scan(
			&it.ID,
			&devID,
			&it.Title,
			&it.Severity,
			&it.Status,
			&it.Source,
			&it.Details,
			&it.CreatedAt,
			&it.UpdatedAt,
			&ackAt,
			&resolvedAt,
			&closedAt,
		); err != nil {
			return nil, err
		}
		if devID.Valid {
			v := int(devID.Int64)
			it.DeviceID = &v
		}
		if ackAt.Valid {
			it.AcknowledgedAt = &ackAt.Time
		}
		if resolvedAt.Valid {
			it.ResolvedAt = &resolvedAt.Time
		}
		if closedAt.Valid {
			it.ClosedAt = &closedAt.Time
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repo) TransitionIncidentStatus(incidentID int64, toStatus, changedBy, comment string) (*domain.Incident, error) {
	if incidentID <= 0 {
		return nil, fmt.Errorf("incident id is required")
	}
	to, err := normalizeIncidentStatus(toStatus)
	if err != nil {
		return nil, err
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	comment = strings.TrimSpace(comment)

	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var fromStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM incidents WHERE id = $1 FOR UPDATE`, incidentID).Scan(&fromStatus); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if fromStatus == to {
		return nil, fmt.Errorf("incident already in status %q", to)
	}
	nextAllowed := allowedIncidentTransitions[fromStatus]
	if _, ok := nextAllowed[to]; !ok {
		return nil, fmt.Errorf("invalid status transition %q -> %q", fromStatus, to)
	}

	query := `UPDATE incidents
              SET status = $1,
                  updated_at = NOW(),
                  acknowledged_at = CASE WHEN $1 = 'acknowledged' AND acknowledged_at IS NULL THEN NOW() ELSE acknowledged_at END,
                  resolved_at = CASE WHEN $1 = 'resolved' THEN NOW() ELSE resolved_at END,
                  closed_at = CASE WHEN $1 = 'closed' THEN NOW() ELSE closed_at END
              WHERE id = $2`
	if _, err := tx.ExecContext(ctx, query, to, incidentID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
         VALUES ($1, $2, $3, $4, $5)`,
		incidentID, fromStatus, to, changedBy, comment,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetIncidentByID(incidentID)
}

func (r *Repo) ListIncidentTransitions(incidentID int64, limit int) ([]domain.IncidentTransition, error) {
	if incidentID <= 0 {
		return nil, fmt.Errorf("incident id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(
		context.Background(),
		`SELECT id, incident_id, from_status, to_status, changed_by, comment, changed_at
         FROM incident_transitions
         WHERE incident_id = $1
         ORDER BY changed_at DESC
         LIMIT $2`,
		incidentID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.IncidentTransition, 0)
	for rows.Next() {
		var tr domain.IncidentTransition
		if err := rows.Scan(&tr.ID, &tr.IncidentID, &tr.FromStatus, &tr.ToStatus, &tr.ChangedBy, &tr.Comment, &tr.ChangedAt); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}
