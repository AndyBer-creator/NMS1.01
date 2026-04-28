package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *Repo) GetIncidentByID(ctx context.Context, id int64) (*domain.Incident, error) {
	return r.getIncidentByIDWithExec(ctx, r.db, id)
}

func (r *Repo) getIncidentByIDWithExec(ctx context.Context, exec sqlExecutor, id int64) (*domain.Incident, error) {
	if id <= 0 {
		return nil, nil
	}
	var out domain.Incident
	var devID sql.NullInt64
	var assignee sql.NullString
	var ackAt, resolvedAt, closedAt sql.NullTime
	err := exec.QueryRowContext(
		ctx,
		`SELECT id, device_id, assignee, title, severity, status, source, details,
                created_at, updated_at, acknowledged_at, resolved_at, closed_at
         FROM incidents WHERE id = $1`,
		id,
	).Scan(
		&out.ID,
		&devID,
		&assignee,
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
	if assignee.Valid {
		a := strings.TrimSpace(assignee.String)
		out.Assignee = &a
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

func (r *Repo) ListIncidents(ctx context.Context, limit int, deviceID *int, status, severity string) ([]domain.Incident, error) {
	page, err := r.ListIncidentsPage(ctx, limit, deviceID, status, severity, nil, nil)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (r *Repo) ListIncidentsPage(ctx context.Context, limit int, deviceID *int, status, severity string, cursorUpdatedAt *time.Time, cursorID *int64) (*IncidentListPage, error) {
	return r.listIncidentsPageWithExec(ctx, r.db, limit, deviceID, status, severity, cursorUpdatedAt, cursorID)
}

func (r *Repo) listIncidentsPageWithExec(ctx context.Context, exec sqlExecutor, limit int, deviceID *int, status, severity string, cursorUpdatedAt *time.Time, cursorID *int64) (*IncidentListPage, error) {
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
	if cursorUpdatedAt != nil && cursorID != nil && *cursorID > 0 {
		args = append(args, *cursorUpdatedAt, *cursorID)
		conds = append(conds, fmt.Sprintf("(updated_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	query := `SELECT id, device_id, assignee, title, severity, status, source, details,
                     created_at, updated_at, acknowledged_at, resolved_at, closed_at
              FROM incidents`
	if len(conds) > 0 {
		// #nosec G202 -- conditions are constructed from fixed column names and placeholders only.
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d", len(args))
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.Incident, 0)
	for rows.Next() {
		it, err := scanIncidentRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page := &IncidentListPage{Items: out}
	if len(page.Items) > limit {
		page.More = true
		page.Items = page.Items[:limit]
	}
	return page, nil
}

type incidentScanner interface {
	Scan(dest ...any) error
}

func scanIncidentRow(row incidentScanner) (*domain.Incident, error) {
	var it domain.Incident
	var devID sql.NullInt64
	var assignee sql.NullString
	var ackAt, resolvedAt, closedAt sql.NullTime
	if err := row.Scan(
		&it.ID,
		&devID,
		&assignee,
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
	if assignee.Valid {
		a := strings.TrimSpace(assignee.String)
		it.Assignee = &a
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
	return &it, nil
}

func (r *Repo) ListIncidentTransitions(ctx context.Context, incidentID int64, limit int) ([]domain.IncidentTransition, error) {
	if incidentID <= 0 {
		return nil, fmt.Errorf("incident id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(
		ctx,
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
