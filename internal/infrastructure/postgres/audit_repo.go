package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SNMPSetAuditRecord describes one SNMP SET audit event row.
type SNMPSetAuditRecord struct {
	UserName string
	DeviceID sql.NullInt64
	OID      string
	OldValue string
	NewValue string
	Result   string
	Error    string
}

func (r *Repo) InsertSNMPSetAudit(ctx context.Context, a SNMPSetAuditRecord) error {
	return r.insertSNMPSetAuditWithExec(ctx, r.db, a)
}

func (r *Repo) insertSNMPSetAuditWithExec(ctx context.Context, exec sqlExecutor, a SNMPSetAuditRecord) error {
	if strings.TrimSpace(a.OID) == "" {
		return fmt.Errorf("snmp set audit: oid is required")
	}
	if strings.TrimSpace(a.Result) == "" {
		a.Result = "unknown"
	}
	_, err := exec.ExecContext(
		ctx,
		`INSERT INTO snmp_set_audit (user_name, device_id, oid, old_value, new_value, result, error)
         VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		strings.TrimSpace(a.UserName),
		a.DeviceID,
		strings.TrimSpace(a.OID),
		a.OldValue,
		a.NewValue,
		strings.TrimSpace(a.Result),
		strings.TrimSpace(a.Error),
	)
	return err
}
