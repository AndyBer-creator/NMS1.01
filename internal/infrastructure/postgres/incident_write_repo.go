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

var incidentStatusOrder = []string{"new", "acknowledged", "in_progress", "resolved", "closed"}

func incidentStatusPath(from, to string) []string {
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	if from == "" || to == "" || from == to {
		return nil
	}
	fromIdx := -1
	toIdx := -1
	for i, s := range incidentStatusOrder {
		if s == from {
			fromIdx = i
		}
		if s == to {
			toIdx = i
		}
	}
	if fromIdx < 0 || toIdx < 0 || toIdx <= fromIdx {
		return nil
	}
	return append([]string(nil), incidentStatusOrder[fromIdx+1:toIdx+1]...)
}

func (r *Repo) insertIncidentInTx(ctx context.Context, tx *sql.Tx, devID sql.NullInt64, title, severity, source string, details json.RawMessage) (int64, error) {
	var assignee sql.NullString
	effectiveAssignee := defaultIncidentAssignee(source, severity)
	if effectiveAssignee != nil && strings.TrimSpace(*effectiveAssignee) != "" {
		assignee = sql.NullString{String: strings.TrimSpace(*effectiveAssignee), Valid: true}
	}
	var id int64
	err := tx.QueryRowContext(
		ctx,
		`INSERT INTO incidents (device_id, assignee, title, severity, status, source, details)
         VALUES ($1, $2, $3, $4, 'new', $5, $6::jsonb)
         RETURNING id`,
		devID, assignee, title, severity, source, []byte(details),
	).Scan(&id)
	return id, err
}

func (r *Repo) updateIncidentStatusInTx(ctx context.Context, tx *sql.Tx, incidentID int64, toStatus string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE incidents
		   SET status = $1,
		       updated_at = NOW(),
		       acknowledged_at = CASE WHEN $1 = 'acknowledged' AND acknowledged_at IS NULL THEN NOW() ELSE acknowledged_at END,
		       resolved_at = CASE WHEN $1 = 'resolved' THEN NOW() ELSE resolved_at END,
		       closed_at = CASE WHEN $1 = 'closed' THEN NOW() ELSE closed_at END
		 WHERE id = $2`, toStatus, incidentID)
	return err
}

func (r *Repo) updateIncidentAssigneeInTx(ctx context.Context, tx *sql.Tx, incidentID int64, assignee sql.NullString) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE incidents
		   SET assignee = $1,
		       updated_at = NOW()
		 WHERE id = $2`, assignee, incidentID)
	return err
}

func (r *Repo) insertIncidentTransitionInTx(ctx context.Context, tx *sql.Tx, incidentID int64, fromStatus, toStatus, changedBy, comment string) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
         VALUES ($1, $2, $3, $4, $5)`,
		incidentID, fromStatus, toStatus, changedBy, comment,
	)
	return err
}

func (r *Repo) CreateIncident(ctx context.Context, input *domain.Incident) (*domain.Incident, error) {
	return r.createIncidentWithExec(ctx, r.db, input)
}

func (r *Repo) createIncidentWithExec(ctx context.Context, exec sqlExecutor, input *domain.Incident) (*domain.Incident, error) {
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
		details = domain.JSONPayload(`{}`)
	}
	var devID sql.NullInt64
	if input.DeviceID != nil && *input.DeviceID > 0 {
		devID = sql.NullInt64{Int64: int64(*input.DeviceID), Valid: true}
	}
	var assignee sql.NullString
	effectiveAssignee := input.Assignee
	if effectiveAssignee == nil || strings.TrimSpace(*effectiveAssignee) == "" {
		effectiveAssignee = defaultIncidentAssignee(source, severity)
	}
	if effectiveAssignee != nil && strings.TrimSpace(*effectiveAssignee) != "" {
		assignee = sql.NullString{String: strings.TrimSpace(*effectiveAssignee), Valid: true}
	}
	var out domain.Incident
	var outDevID sql.NullInt64
	var outAssignee sql.NullString
	var ackAt, resolvedAt, closedAt sql.NullTime
	if err := exec.QueryRowContext(
		ctx,
		`INSERT INTO incidents (device_id, assignee, title, severity, status, source, details)
         VALUES ($1, $2, $3, $4, 'new', $5, $6::jsonb)
         RETURNING id, device_id, assignee, title, severity, status, source, details,
                   created_at, updated_at, acknowledged_at, resolved_at, closed_at`,
		devID, assignee, title, severity, source, []byte(details),
	).Scan(
		&out.ID,
		&outDevID,
		&outAssignee,
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
	if outAssignee.Valid {
		a := strings.TrimSpace(outAssignee.String)
		out.Assignee = &a
	}
	return &out, nil
}

func (r *Repo) TransitionIncidentStatus(ctx context.Context, incidentID int64, toStatus, changedBy, comment string) (*domain.Incident, error) {
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

	var notFound bool
	if err := r.InTx(ctx, func(tx *sql.Tx) error {
		var fromStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM incidents WHERE id = $1 FOR UPDATE`, incidentID).Scan(&fromStatus); err != nil {
			if err == sql.ErrNoRows {
				notFound = true
				return nil
			}
			return err
		}
		from := strings.ToLower(strings.TrimSpace(fromStatus))
		if from == to {
			return fmt.Errorf("incident already in status %q", to)
		}
		steps := incidentStatusPath(from, to)
		if len(steps) == 0 {
			return fmt.Errorf("invalid status transition %q -> %q", from, to)
		}
		prev := from
		for _, step := range steps {
			nextAllowed := allowedIncidentTransitions[prev]
			if _, ok := nextAllowed[step]; !ok {
				return fmt.Errorf("invalid status transition %q -> %q", prev, step)
			}
			if err := r.updateIncidentStatusInTx(ctx, tx, incidentID, step); err != nil {
				return err
			}
			if err := r.insertIncidentTransitionInTx(ctx, tx, incidentID, prev, step, changedBy, comment); err != nil {
				return err
			}
			prev = step
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if notFound {
		return nil, nil
	}
	return r.GetIncidentByID(ctx, incidentID)
}

func (r *Repo) AssignIncident(ctx context.Context, incidentID int64, assignee, changedBy, comment string) (*domain.Incident, error) {
	if incidentID <= 0 {
		return nil, fmt.Errorf("incident id is required")
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	comment = strings.TrimSpace(comment)
	trimmedAssignee := strings.TrimSpace(assignee)
	var target sql.NullString
	if trimmedAssignee != "" {
		target = sql.NullString{String: trimmedAssignee, Valid: true}
	}
	var notFound bool
	if err := r.InTx(ctx, func(tx *sql.Tx) error {
		var fromStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM incidents WHERE id = $1 FOR UPDATE`, incidentID).Scan(&fromStatus); err != nil {
			if err == sql.ErrNoRows {
				notFound = true
				return nil
			}
			return err
		}
		if err := r.updateIncidentAssigneeInTx(ctx, tx, incidentID, target); err != nil {
			return err
		}
		auditComment := comment
		if auditComment == "" {
			if target.Valid {
				auditComment = "assigned to " + target.String
			} else {
				auditComment = "assignee cleared"
			}
		}
		if err := r.insertIncidentTransitionInTx(ctx, tx, incidentID, fromStatus, fromStatus, changedBy, auditComment); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if notFound {
		return nil, nil
	}
	return r.GetIncidentByID(ctx, incidentID)
}

func (r *Repo) CreateOrTouchOpenIncident(ctx context.Context, deviceID *int, title, severity, source string, details json.RawMessage, suppressionWindow time.Duration) (*domain.Incident, bool, error) {
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
	lockKey := incidentDedupLockKey(devID, title, sv, src)
	var touchedID int64
	created := false
	err = r.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
			return err
		}
		updateErr := tx.QueryRowContext(ctx, `
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
		if updateErr == nil {
			return nil
		}
		if updateErr != sql.ErrNoRows {
			return updateErr
		}
		id, err := r.insertIncidentInTx(ctx, tx, devID, title, sv, src, details)
		if err != nil {
			return err
		}
		touchedID = id
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if touchedID <= 0 {
		return nil, false, fmt.Errorf("incident create/touch invariant violated")
	}
	item, gerr := r.GetIncidentByID(ctx, touchedID)
	return item, created, gerr
}

func (r *Repo) ApplyITSMInboundUpdate(ctx context.Context, incidentID int64, toStatus, assignee, changedBy, comment string) (*domain.Incident, bool, bool, error) {
	if incidentID <= 0 {
		return nil, false, false, fmt.Errorf("incident id is required")
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	comment = strings.TrimSpace(comment)
	trimmedAssignee := strings.TrimSpace(assignee)
	statusRequested := strings.TrimSpace(toStatus) != ""
	if statusRequested {
		var err error
		toStatus, err = normalizeIncidentStatus(toStatus)
		if err != nil {
			return nil, false, false, err
		}
	}

	var out domain.Incident
	var devID sql.NullInt64
	var currentAssignee sql.NullString
	var ackAt, resolvedAt, closedAt sql.NullTime
	statusChanged := false
	assigneeChanged := false
	var notFound bool
	err := r.InTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			SELECT id, device_id, assignee, title, severity, status, source, details,
			       created_at, updated_at, acknowledged_at, resolved_at, closed_at
			  FROM incidents
			 WHERE id = $1
			 FOR UPDATE`, incidentID,
		).Scan(
			&out.ID,
			&devID,
			&currentAssignee,
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
			if err == sql.ErrNoRows {
				notFound = true
				return nil
			}
			return err
		}

		fromStatus := strings.ToLower(strings.TrimSpace(out.Status))
		currentAssigneeStr := strings.TrimSpace(currentAssignee.String)

		if statusRequested && toStatus != fromStatus {
			nextAllowed := allowedIncidentTransitions[fromStatus]
			if _, ok := nextAllowed[toStatus]; !ok {
				return fmt.Errorf("invalid status transition %q -> %q", fromStatus, toStatus)
			}
			if err := r.updateIncidentStatusInTx(ctx, tx, incidentID, toStatus); err != nil {
				return err
			}
			if err := r.insertIncidentTransitionInTx(ctx, tx, incidentID, fromStatus, toStatus, changedBy, comment); err != nil {
				return err
			}
			out.Status = toStatus
			statusChanged = true
		}

		if trimmedAssignee != currentAssigneeStr {
			var target sql.NullString
			if trimmedAssignee != "" {
				target = sql.NullString{String: trimmedAssignee, Valid: true}
			}
			if err := r.updateIncidentAssigneeInTx(ctx, tx, incidentID, target); err != nil {
				return err
			}
			auditComment := comment
			if auditComment == "" {
				if target.Valid {
					auditComment = "assigned to " + target.String
				} else {
					auditComment = "assignee cleared"
				}
			}
			if err := r.insertIncidentTransitionInTx(ctx, tx, incidentID, out.Status, out.Status, changedBy, auditComment); err != nil {
				return err
			}
			assigneeChanged = true
		}
		return nil
	})
	if err != nil {
		return nil, false, false, err
	}
	if notFound {
		return nil, false, false, nil
	}
	item, err := r.GetIncidentByID(ctx, incidentID)
	if err != nil {
		return nil, false, false, err
	}
	return item, statusChanged, assigneeChanged, nil
}

func (r *Repo) EscalateUnackedIncidents(ctx context.Context, olderThan time.Duration, targetAssignee, changedBy, comment string) (int64, error) {
	return r.EscalateUnackedIncidentsWithFilter(ctx, olderThan, targetAssignee, changedBy, comment, "", "", false)
}

func (r *Repo) EscalateUnackedIncidentsWithFilter(ctx context.Context, olderThan time.Duration, targetAssignee, changedBy, comment, severity, source string, onlyIfUnassigned bool) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	targetAssignee = strings.TrimSpace(targetAssignee)
	if targetAssignee == "" {
		return 0, nil
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system-escalation"
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "auto-escalated: ack timeout reached"
	}
	var err error
	if strings.TrimSpace(severity) != "" {
		severity, err = normalizeIncidentSeverity(severity)
		if err != nil {
			return 0, err
		}
	}
	if strings.TrimSpace(source) != "" {
		source, err = normalizeIncidentSource(source)
		if err != nil {
			return 0, err
		}
	}

	var changed int64
	if err := r.db.QueryRowContext(ctx, `
		WITH candidates AS (
			SELECT id, status
			  FROM incidents
			 WHERE status = 'new'
			   AND created_at <= NOW() - make_interval(secs => $1)
			   AND (assignee IS DISTINCT FROM $2)
			   AND ($3 = '' OR severity = $3)
			   AND ($4 = '' OR source = $4)
			   AND (NOT $5 OR assignee IS NULL OR btrim(assignee) = '')
			 FOR UPDATE
		),
		updated AS (
			UPDATE incidents i
			   SET assignee = $2,
			       updated_at = NOW()
			  FROM candidates c
			 WHERE i.id = c.id
			RETURNING i.id, c.status AS from_status
		),
		inserted AS (
			INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
			SELECT id, from_status, from_status, $6, $7
			  FROM updated
		)
		SELECT COUNT(*) FROM updated
	`, olderThan.Seconds(), targetAssignee, severity, source, onlyIfUnassigned, changedBy, comment).Scan(&changed); err != nil {
		return 0, err
	}
	return changed, nil
}

func (r *Repo) TransitionIncidentsStatus(ctx context.Context, incidentIDs []int64, toStatus, changedBy, comment string) ([]domain.Incident, error) {
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
		item, err := r.TransitionIncidentStatus(ctx, id, toStatus, changedBy, comment)
		if err != nil {
			return nil, err
		}
		if item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}
