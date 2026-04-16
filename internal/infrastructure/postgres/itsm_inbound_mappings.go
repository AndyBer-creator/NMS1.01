package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func normalizeITSMProvider(v string) string {
	p := strings.ToLower(strings.TrimSpace(v))
	if p == "" {
		return "generic"
	}
	return p
}

func normalizeMappedIncidentStatus(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return "", nil
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
		return "", fmt.Errorf("invalid mapped_status %q", v)
	}
}

func (r *Repo) ListITSMInboundMappings(ctx context.Context, provider string, enabled *bool) ([]domain.ITSMInboundMapping, error) {
	args := make([]any, 0, 2)
	conds := make([]string, 0, 2)
	if p := strings.TrimSpace(provider); p != "" {
		args = append(args, strings.ToLower(p))
		conds = append(conds, fmt.Sprintf("lower(provider) = $%d", len(args)))
	}
	if enabled != nil {
		args = append(args, *enabled)
		conds = append(conds, fmt.Sprintf("enabled = $%d", len(args)))
	}
	query := `SELECT id, provider, external_status, external_priority, external_owner,
	                 mapped_status, mapped_assignee, enabled, priority, created_at
	            FROM itsm_inbound_mappings`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY priority ASC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.ITSMInboundMapping, 0)
	for rows.Next() {
		var it domain.ITSMInboundMapping
		if err := rows.Scan(
			&it.ID, &it.Provider, &it.ExternalStatus, &it.ExternalPriority, &it.ExternalOwner,
			&it.MappedStatus, &it.MappedAssignee, &it.Enabled, &it.Priority, &it.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repo) CreateITSMInboundMapping(ctx context.Context, in *domain.ITSMInboundMapping) (*domain.ITSMInboundMapping, error) {
	if in == nil {
		return nil, fmt.Errorf("mapping input is required")
	}
	provider := normalizeITSMProvider(in.Provider)
	extStatus := strings.ToLower(strings.TrimSpace(in.ExternalStatus))
	extPriority := strings.ToLower(strings.TrimSpace(in.ExternalPriority))
	extOwner := strings.ToLower(strings.TrimSpace(in.ExternalOwner))
	mappedStatus, err := normalizeMappedIncidentStatus(in.MappedStatus)
	if err != nil {
		return nil, err
	}
	mappedAssignee := strings.TrimSpace(in.MappedAssignee)
	if mappedStatus == "" && mappedAssignee == "" {
		return nil, fmt.Errorf("at least one of mapped_status or mapped_assignee is required")
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	var out domain.ITSMInboundMapping
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO itsm_inbound_mappings
		    (provider, external_status, external_priority, external_owner, mapped_status, mapped_assignee, enabled, priority)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, provider, external_status, external_priority, external_owner,
		          mapped_status, mapped_assignee, enabled, priority, created_at`,
		provider, extStatus, extPriority, extOwner, mappedStatus, mappedAssignee, in.Enabled, priority,
	).Scan(
		&out.ID, &out.Provider, &out.ExternalStatus, &out.ExternalPriority, &out.ExternalOwner,
		&out.MappedStatus, &out.MappedAssignee, &out.Enabled, &out.Priority, &out.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repo) UpdateITSMInboundMapping(ctx context.Context, id int64, in *domain.ITSMInboundMapping) (*domain.ITSMInboundMapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	if in == nil {
		return nil, fmt.Errorf("mapping input is required")
	}
	provider := normalizeITSMProvider(in.Provider)
	extStatus := strings.ToLower(strings.TrimSpace(in.ExternalStatus))
	extPriority := strings.ToLower(strings.TrimSpace(in.ExternalPriority))
	extOwner := strings.ToLower(strings.TrimSpace(in.ExternalOwner))
	mappedStatus, err := normalizeMappedIncidentStatus(in.MappedStatus)
	if err != nil {
		return nil, err
	}
	mappedAssignee := strings.TrimSpace(in.MappedAssignee)
	if mappedStatus == "" && mappedAssignee == "" {
		return nil, fmt.Errorf("at least one of mapped_status or mapped_assignee is required")
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	var out domain.ITSMInboundMapping
	err = r.db.QueryRowContext(ctx, `
		UPDATE itsm_inbound_mappings
		   SET provider = $1,
		       external_status = $2,
		       external_priority = $3,
		       external_owner = $4,
		       mapped_status = $5,
		       mapped_assignee = $6,
		       enabled = $7,
		       priority = $8
		 WHERE id = $9
		RETURNING id, provider, external_status, external_priority, external_owner,
		          mapped_status, mapped_assignee, enabled, priority, created_at`,
		provider, extStatus, extPriority, extOwner, mappedStatus, mappedAssignee, in.Enabled, priority, id,
	).Scan(
		&out.ID, &out.Provider, &out.ExternalStatus, &out.ExternalPriority, &out.ExternalOwner,
		&out.MappedStatus, &out.MappedAssignee, &out.Enabled, &out.Priority, &out.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repo) DeleteITSMInboundMapping(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("mapping id is required")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM itsm_inbound_mappings WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *Repo) ResolveITSMInboundMapping(ctx context.Context, provider, externalStatus, externalPriority, externalOwner string) (*domain.ITSMInboundMapping, error) {
	provider = normalizeITSMProvider(provider)
	externalStatus = strings.ToLower(strings.TrimSpace(externalStatus))
	externalPriority = strings.ToLower(strings.TrimSpace(externalPriority))
	externalOwner = strings.ToLower(strings.TrimSpace(externalOwner))

	var out domain.ITSMInboundMapping
	err := r.db.QueryRowContext(ctx, `
		SELECT id, provider, external_status, external_priority, external_owner,
		       mapped_status, mapped_assignee, enabled, priority, created_at
		  FROM itsm_inbound_mappings
		 WHERE enabled = TRUE
		   AND (provider = '' OR lower(provider) = $1)
		   AND (external_status = '' OR lower(external_status) = $2)
		   AND (external_priority = '' OR lower(external_priority) = $3)
		   AND (external_owner = '' OR lower(external_owner) = $4)
		 ORDER BY priority ASC, id ASC
		 LIMIT 1`,
		provider, externalStatus, externalPriority, externalOwner,
	).Scan(
		&out.ID,
		&out.Provider,
		&out.ExternalStatus,
		&out.ExternalPriority,
		&out.ExternalOwner,
		&out.MappedStatus,
		&out.MappedAssignee,
		&out.Enabled,
		&out.Priority,
		&out.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
