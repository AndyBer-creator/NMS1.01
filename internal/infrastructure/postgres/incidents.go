package postgres

import (
	"NMS1/internal/config"
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"strings"
)

// IncidentListPage contains paginated incident rows with "more" marker.
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

func incidentDedupLockKey(deviceID sql.NullInt64, title, severity, source string) int64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "incident|src=%s|sev=%s|title=%s|dev_valid=%t|dev_id=%d",
		strings.TrimSpace(strings.ToLower(source)),
		strings.TrimSpace(strings.ToLower(severity)),
		strings.TrimSpace(strings.ToLower(title)),
		deviceID.Valid,
		deviceID.Int64,
	)
	return int64(h.Sum64())
}

func defaultIncidentAssignee(source, severity string) *string {
	// Priority:
	// 1) critical-specific
	// 2) source-specific
	// 3) global default
	if strings.EqualFold(strings.TrimSpace(severity), "critical") {
		if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_CRITICAL")); v != "" {
			return &v
		}
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "trap":
		if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_TRAP")); v != "" {
			return &v
		}
	case "polling":
		if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_POLLING")); v != "" {
			return &v
		}
	case "manual":
		if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_MANUAL")); v != "" {
			return &v
		}
	}
	if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_DEFAULT")); v != "" {
		return &v
	}
	return nil
}




func (r *Repo) ResolveOpenIncidentsBySource(ctx context.Context, deviceID int, source, changedBy, comment string) (int64, error) {
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
	var changed int64
	if err := r.db.QueryRowContext(ctx, `
		WITH candidates AS (
			SELECT id, status
			  FROM incidents
			 WHERE device_id = $1
			   AND source = $2
			   AND status IN ('new', 'acknowledged', 'in_progress')
			 FOR UPDATE
		),
		updated AS (
			UPDATE incidents i
			   SET status = 'resolved',
			       updated_at = NOW(),
			       resolved_at = NOW()
			  FROM candidates c
			 WHERE i.id = c.id
			RETURNING i.id, c.status AS from_status
		),
		inserted AS (
			INSERT INTO incident_transitions (incident_id, from_status, to_status, changed_by, comment)
			SELECT id, from_status, 'resolved', $3, $4
			  FROM updated
		)
		SELECT COUNT(*) FROM updated
	`, deviceID, src, changedBy, comment).Scan(&changed); err != nil {
		return 0, err
	}
	return changed, nil
}



