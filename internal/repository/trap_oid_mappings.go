package repository

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var allowedTrapSignalKinds = map[string]struct{}{
	"generic":   {},
	"link_down": {},
	"link_up":   {},
	"bfd_down":  {},
	"bfd_up":    {},
}

func normalizeTrapMappingSignalKind(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	if _, ok := allowedTrapSignalKinds[s]; !ok {
		return "", fmt.Errorf("invalid signal_kind %q", v)
	}
	return s, nil
}

func normalizeTrapMappingSeverity(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "critical", "warning", "info":
		return s, nil
	default:
		return "", fmt.Errorf("invalid severity %q", v)
	}
}

func (r *TrapsRepo) ListOIDMappings(ctx context.Context, vendor string, enabled *bool) ([]domain.TrapOIDMapping, error) {
	args := make([]any, 0, 2)
	conds := make([]string, 0, 2)
	if v := strings.TrimSpace(vendor); v != "" {
		args = append(args, strings.ToLower(v))
		conds = append(conds, fmt.Sprintf("vendor = $%d", len(args)))
	}
	if enabled != nil {
		args = append(args, *enabled)
		conds = append(conds, fmt.Sprintf("enabled = $%d", len(args)))
	}
	query := `SELECT id, vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled, created_at
              FROM trap_oid_mappings`
	if len(conds) > 0 {
		// #nosec G202 -- conditions are constructed from fixed column names and placeholders only.
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY priority DESC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.TrapOIDMapping, 0)
	for rows.Next() {
		var it domain.TrapOIDMapping
		if err := rows.Scan(
			&it.ID,
			&it.Vendor,
			&it.OIDPattern,
			&it.SignalKind,
			&it.Title,
			&it.Severity,
			&it.IsRecovery,
			&it.Priority,
			&it.Enabled,
			&it.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *TrapsRepo) CreateOIDMapping(ctx context.Context, in *domain.TrapOIDMapping) (*domain.TrapOIDMapping, error) {
	if in == nil {
		return nil, fmt.Errorf("mapping input is required")
	}
	vendor := strings.ToLower(strings.TrimSpace(in.Vendor))
	if vendor == "" {
		vendor = "generic"
	}
	pattern := strings.ToLower(strings.TrimSpace(in.OIDPattern))
	if pattern == "" {
		return nil, fmt.Errorf("oid_pattern is required")
	}
	signalKind, err := normalizeTrapMappingSignalKind(in.SignalKind)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	severity, err := normalizeTrapMappingSeverity(in.Severity)
	if err != nil {
		return nil, err
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	enabled := in.Enabled
	var out domain.TrapOIDMapping
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO trap_oid_mappings (vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled, created_at`,
		vendor, pattern, signalKind, title, severity, in.IsRecovery, priority, enabled,
	).Scan(
		&out.ID,
		&out.Vendor,
		&out.OIDPattern,
		&out.SignalKind,
		&out.Title,
		&out.Severity,
		&out.IsRecovery,
		&out.Priority,
		&out.Enabled,
		&out.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *TrapsRepo) UpdateOIDMapping(ctx context.Context, id int64, in *domain.TrapOIDMapping) (*domain.TrapOIDMapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	if in == nil {
		return nil, fmt.Errorf("mapping input is required")
	}
	vendor := strings.ToLower(strings.TrimSpace(in.Vendor))
	if vendor == "" {
		vendor = "generic"
	}
	pattern := strings.ToLower(strings.TrimSpace(in.OIDPattern))
	if pattern == "" {
		return nil, fmt.Errorf("oid_pattern is required")
	}
	signalKind, err := normalizeTrapMappingSignalKind(in.SignalKind)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	severity, err := normalizeTrapMappingSeverity(in.Severity)
	if err != nil {
		return nil, err
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	var out domain.TrapOIDMapping
	err = r.db.QueryRowContext(ctx, `
		UPDATE trap_oid_mappings
		   SET vendor = $1,
		       oid_pattern = $2,
		       signal_kind = $3,
		       title = $4,
		       severity = $5,
		       is_recovery = $6,
		       priority = $7,
		       enabled = $8
		 WHERE id = $9
		RETURNING id, vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled, created_at`,
		vendor, pattern, signalKind, title, severity, in.IsRecovery, priority, in.Enabled, id,
	).Scan(
		&out.ID,
		&out.Vendor,
		&out.OIDPattern,
		&out.SignalKind,
		&out.Title,
		&out.Severity,
		&out.IsRecovery,
		&out.Priority,
		&out.Enabled,
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

func (r *TrapsRepo) DeleteOIDMapping(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("mapping id is required")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM trap_oid_mappings WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
