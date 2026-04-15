package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type IncidentListPage struct {
	Items []domain.Incident
	More  bool
}

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
	page, err := r.ListIncidentsPage(limit, deviceID, status, severity, nil, nil)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (r *Repo) ListIncidentsPage(limit int, deviceID *int, status, severity string, cursorUpdatedAt *time.Time, cursorID *int64) (*IncidentListPage, error) {
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
	query := `SELECT id, device_id, title, severity, status, source, details,
                     created_at, updated_at, acknowledged_at, resolved_at, closed_at
              FROM incidents`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d", len(args))
	rows, err := r.db.QueryContext(context.Background(), query, args...)
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

func (r *Repo) CreateOrTouchOpenIncident(deviceID *int, title, severity, source string, details json.RawMessage, suppressionWindow time.Duration) (*domain.Incident, bool, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, false, fmt.Errorf("title is required")
	}
	sv, err := normalizeIncidentSeverity(severity)
	if err != nil {
		return nil, false, err
	}
	src, err := normalizeIncidentSource(source)
	if err != nil {
		return nil, false, err
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
	var devID sql.NullInt64
	if deviceID != nil && *deviceID > 0 {
		devID = sql.NullInt64{Int64: int64(*deviceID), Valid: true}
	}

	var touchedID int64
	err = r.db.QueryRowContext(context.Background(), `
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
		item, gerr := r.GetIncidentByID(touchedID)
		return item, false, gerr
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	item, err := r.CreateIncident(&domain.Incident{
		DeviceID: deviceID,
		Title:    title,
		Severity: sv,
		Source:   src,
		Details:  details,
	})
	if err != nil {
		return nil, false, err
	}
	return item, true, nil
}

func (r *Repo) ResolveOpenIncidentsBySource(deviceID int, source, changedBy, comment string) (int64, error) {
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
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

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
	var ids []int64
	var fromStatuses []string
	for rows.Next() {
		var id int64
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			return 0, err
		}
		ids = append(ids, id)
		fromStatuses = append(fromStatuses, st)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var changed int64
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE incidents
			   SET status = 'resolved',
			       updated_at = NOW(),
			       resolved_at = NOW()
			 WHERE id = $1`, id); err != nil {
			return changed, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
			VALUES ($1, $2, 'resolved', $3, $4)`,
			id, fromStatuses[i], changedBy, comment,
		); err != nil {
			return changed, err
		}
		changed++
	}
	if err := tx.Commit(); err != nil {
		return changed, err
	}
	return changed, nil
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

func (r *Repo) TransitionIncidentsStatus(incidentIDs []int64, toStatus, changedBy, comment string) ([]domain.Incident, error) {
	out := make([]domain.Incident, 0, len(incidentIDs))
	seen := map[int64]struct{}{}
	for _, id := range incidentIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		item, err := r.TransitionIncidentStatus(id, toStatus, changedBy, comment)
		if err != nil {
			return nil, err
		}
		if item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}

type incidentScanner interface {
	Scan(dest ...any) error
}

func scanIncidentRow(row incidentScanner) (*domain.Incident, error) {
	var it domain.Incident
	var devID sql.NullInt64
	var ackAt, resolvedAt, closedAt sql.NullTime
	if err := row.Scan(
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
	return &it, nil
}
