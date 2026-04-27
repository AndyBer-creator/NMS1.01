package repository

import (
	"NMS1/internal/config"
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
)

// TrapsRepo persists traps and trap-driven incident correlation state.
type TrapsRepo struct {
	db *sql.DB
}

var _ TrapRepository = (*TrapsRepo)(nil)

// NewTrapsRepo creates a trap repository over an existing SQL connection.
func NewTrapsRepo(db *sql.DB) *TrapsRepo {
	return &TrapsRepo{db: db}
}

// List returns latest traps ordered by received_at descending.
func (r *TrapsRepo) List(ctx context.Context, limit int) ([]domain.Trap, error) {
	query := `
        SELECT id, device_ip, oid, uptime, trap_vars, received_at 
        FROM traps 
        ORDER BY received_at DESC LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	traps := make([]domain.Trap, 0)
	for rows.Next() {
		var t domain.Trap
		err := rows.Scan(&t.ID, &t.DeviceIP, &t.OID, &t.Uptime, &t.TrapVars, &t.ReceivedAt)
		if err != nil {
			return nil, err
		}
		traps = append(traps, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return traps, nil
}

// ByDevice returns latest traps for a specific device IP.
func (r *TrapsRepo) ByDevice(ctx context.Context, ip string, limit int) ([]domain.Trap, error) {
	query := `
        SELECT id, device_ip, oid, uptime, trap_vars, received_at
        FROM traps
        WHERE device_ip = $1
        ORDER BY received_at DESC LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, ip, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	traps := make([]domain.Trap, 0)
	for rows.Next() {
		var t domain.Trap
		err := rows.Scan(&t.ID, &t.DeviceIP, &t.OID, &t.Uptime, &t.TrapVars, &t.ReceivedAt)
		if err != nil {
			return nil, err
		}
		traps = append(traps, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return traps, nil
}

// Insert writes one trap event into traps table.
func (r *TrapsRepo) Insert(ctx context.Context, deviceIP, oid string, uptime int64, trapVars map[string]string, isCritical bool) error {
	if oid == "" {
		oid = "unknown"
	}
	varsJSON, err := json.Marshal(trapVars)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO traps (device_ip, oid, uptime, trap_vars, is_critical)
         VALUES ($1, $2, $3, $4::jsonb, $5)`,
		deviceIP, oid, uptime, varsJSON, isCritical,
	)
	return err
}

type trapSignalKind int

const (
	trapSignalGeneric trapSignalKind = iota
	trapSignalLinkDown
	trapSignalLinkUp
	trapSignalBFDDown
	trapSignalBFDUp
)

type trapClassification struct {
	SignalKind trapSignalKind
	Title      string
	Severity   string
	Recovery   bool
}

type trapOIDMappingRow struct {
	SignalKind string
	Title      string
	Severity   string
	Recovery   bool
}

func normalizeTrapVendor(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return ""
	}
	return s
}

func trapVendorsFromInput(oid string, trapVars map[string]string) []string {
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	add := func(v string) {
		v = normalizeTrapVendor(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for k, v := range trapVars {
		key := strings.ToLower(strings.TrimSpace(k))
		switch key {
		case "vendor", "manufacturer", "enterprise", "enterprise_oid", "sysobjectid", "sysobjectid.0":
			add(v)
		}
	}
	if i := strings.Index(strings.TrimSpace(oid), "::"); i > 0 {
		add(oid[:i])
	}
	add("generic")
	return out
}

func trapIncidentDedupLockKey(deviceID sql.NullInt64, title string) int64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "trap-incident|title=%s|dev_valid=%t|dev_id=%d",
		strings.TrimSpace(strings.ToLower(title)),
		deviceID.Valid,
		deviceID.Int64,
	)
	return int64(h.Sum64())
}

func trapSignalFromOID(oid string) trapSignalKind {
	o := strings.ToLower(strings.TrimSpace(oid))
	switch {
	case strings.Contains(o, "linkdown"):
		return trapSignalLinkDown
	case strings.Contains(o, "linkup"):
		return trapSignalLinkUp
	case strings.Contains(o, "bfddown"), strings.Contains(o, "bfd.down"), strings.Contains(o, "bfd_down"):
		return trapSignalBFDDown
	case strings.Contains(o, "bfdup"), strings.Contains(o, "bfd.up"), strings.Contains(o, "bfd_up"):
		return trapSignalBFDUp
	default:
		return trapSignalGeneric
	}
}

func trapSignalKindFromString(v string) trapSignalKind {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "link_down":
		return trapSignalLinkDown
	case "link_up":
		return trapSignalLinkUp
	case "bfd_down":
		return trapSignalBFDDown
	case "bfd_up":
		return trapSignalBFDUp
	default:
		return trapSignalGeneric
	}
}

func trapSignalKindString(v trapSignalKind) string {
	switch v {
	case trapSignalLinkDown:
		return "link_down"
	case trapSignalLinkUp:
		return "link_up"
	case trapSignalBFDDown:
		return "bfd_down"
	case trapSignalBFDUp:
		return "bfd_up"
	default:
		return "generic"
	}
}

func defaultTrapClassification(oid string) trapClassification {
	return trapClassification{
		SignalKind: trapSignalFromOID(oid),
		Title:      trapIncidentTitle(oid),
		Severity:   trapIncidentSeverity(oid),
		Recovery:   trapIsRecoverySignal(oid),
	}
}

func normalizeIncidentSeverityInput(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "critical":
		return "critical"
	case "info":
		return "info"
	default:
		return "warning"
	}
}

func classificationFromMapping(oid string, row trapOIDMappingRow) trapClassification {
	base := defaultTrapClassification(oid)
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = base.Title
	}
	return trapClassification{
		SignalKind: trapSignalKindFromString(row.SignalKind),
		Title:      title,
		Severity:   normalizeIncidentSeverityInput(row.Severity),
		Recovery:   row.Recovery,
	}
}

func (r *TrapsRepo) trapOIDMappingLookup(ctx context.Context, oid string, trapVars map[string]string) (*trapOIDMappingRow, error) {
	o := strings.TrimSpace(strings.ToLower(oid))
	if o == "" {
		return nil, nil
	}
	var row trapOIDMappingRow
	vendors := trapVendorsFromInput(oid, trapVars)
	vendorPlaceholders := make([]string, 0, len(vendors))
	args := make([]any, 0, len(vendors)+2)
	args = append(args, o)
	for _, vendor := range vendors {
		args = append(args, vendor)
		vendorPlaceholders = append(vendorPlaceholders, fmt.Sprintf("$%d", len(args)))
	}
	args = append(args, vendors[0])
	err := r.db.QueryRowContext(ctx, `
		SELECT signal_kind, title, severity, is_recovery
		  FROM trap_oid_mappings
		 WHERE enabled = TRUE
		   AND vendor IN (`+strings.Join(vendorPlaceholders, ", ")+`)
		   AND $1 ILIKE oid_pattern
		 ORDER BY
		       CASE
		           WHEN vendor = $`+strconv.Itoa(len(args))+` THEN 0
		           WHEN vendor = 'generic' THEN 1
		           ELSE 2
		       END,
		       priority DESC,
		       id ASC
		 LIMIT 1`,
		args...,
	).Scan(&row.SignalKind, &row.Title, &row.Severity, &row.Recovery)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func trapIncidentSeverity(oid string) string {
	switch trapSignalFromOID(oid) {
	case trapSignalLinkDown, trapSignalBFDDown:
		return "critical"
	case trapSignalLinkUp, trapSignalBFDUp:
		return "info"
	default:
		o := strings.ToLower(strings.TrimSpace(oid))
		if strings.Contains(o, "coldstart") {
			return "critical"
		}
		return "warning"
	}
}

func trapIncidentTitle(oid string) string {
	switch trapSignalFromOID(oid) {
	case trapSignalLinkDown, trapSignalBFDDown:
		// Correlate linkDown + BFD down into one incident class.
		return "Link loss detected"
	case trapSignalLinkUp, trapSignalBFDUp:
		return "Link recovery detected"
	default:
		if strings.TrimSpace(oid) == "" || strings.EqualFold(strings.TrimSpace(oid), "unknown") {
			return "SNMP trap: unknown"
		}
		return "SNMP trap: " + strings.TrimSpace(oid)
	}
}

func trapIsRecoverySignal(oid string) bool {
	switch trapSignalFromOID(oid) {
	case trapSignalLinkUp, trapSignalBFDUp:
		return true
	default:
		return false
	}
}

// CreateOrTouchOpenTrapIncident suppresses duplicate trap incidents inside a time window.
func (r *TrapsRepo) CreateOrTouchOpenTrapIncident(ctx context.Context, deviceIP, oid string, trapVars map[string]string, suppressionWindow time.Duration) error {
	if suppressionWindow <= 0 {
		suppressionWindow = 10 * time.Minute
	}
	classification := defaultTrapClassification(oid)
	row, err := r.trapOIDMappingLookup(ctx, oid, trapVars)
	if err != nil {
		return err
	}
	if row != nil {
		classification = classificationFromMapping(oid, *row)
	}
	title := classification.Title
	severity := classification.Severity
	windowSec := int(suppressionWindow.Seconds())
	if windowSec < 1 {
		windowSec = 1
	}

	var devID sql.NullInt64
	_ = r.db.QueryRowContext(ctx, `SELECT id FROM devices WHERE ip = $1`, deviceIP).Scan(&devID)
	varsJSON, _ := json.Marshal(trapVars)
	detailsJSON, _ := json.Marshal(map[string]any{
		"oid":       oid,
		"device_ip": deviceIP,
		"vars":      json.RawMessage(varsJSON),
	})

	// Recovery trap resolves open link-loss incidents and doesn't create noise incident.
	if classification.Recovery {
		_, err := r.ResolveOpenTrapIncidents(ctx, devID, []string{"Link loss detected"}, "system", "auto-resolved by recovery trap")
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, trapIncidentDedupLockKey(devID, title)); err != nil {
		return err
	}

	var touchedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE incidents i
		   SET updated_at = NOW(),
		       details = $1::jsonb
		 WHERE i.id = (
		     SELECT id
		       FROM incidents
		      WHERE source = 'trap'
		        AND status IN ('new', 'acknowledged', 'in_progress')
		        AND title = $2
		        AND (device_id IS NOT DISTINCT FROM $3)
		        AND updated_at >= NOW() - make_interval(secs => $4)
		      ORDER BY updated_at DESC
		      LIMIT 1
		 )
		RETURNING i.id`,
		string(detailsJSON), title, devID, float64(windowSec),
	).Scan(&touchedID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		return tx.Commit()
	}
	var assignee sql.NullString
	if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_CRITICAL")); v != "" && strings.EqualFold(severity, "critical") {
		assignee = sql.NullString{String: v, Valid: true}
	} else if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_TRAP")); v != "" {
		assignee = sql.NullString{String: v, Valid: true}
	} else if v := strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_DEFAULT")); v != "" {
		assignee = sql.NullString{String: v, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO incidents (device_id, assignee, title, severity, status, source, details)
		VALUES ($1, $2, $3, $4, 'new', 'trap', $5::jsonb)`,
		devID, assignee, title, severity, string(detailsJSON),
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TrapsRepo) ResolveOpenTrapIncidents(ctx context.Context, deviceID sql.NullInt64, titles []string, changedBy, comment string) (int64, error) {
	if !deviceID.Valid {
		return 0, nil
	}
	if len(titles) == 0 {
		return 0, nil
	}
	changedBy = strings.TrimSpace(changedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "auto-resolved"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	args := []any{deviceID}
	titleConds := make([]string, 0, len(titles))
	for _, t := range titles {
		tt := strings.TrimSpace(t)
		if tt == "" {
			continue
		}
		args = append(args, tt)
		titleConds = append(titleConds, "title = $"+strconv.Itoa(len(args)))
	}
	if len(titleConds) == 0 {
		return 0, nil
	}
	var changed int64
	query := `
		WITH candidates AS (
			SELECT id, status
			  FROM incidents
			 WHERE source = 'trap'
			   AND device_id = $1
			   AND status IN ('new', 'acknowledged', 'in_progress')
			   AND (` + strings.Join(titleConds, " OR ") + `)
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
			SELECT id, from_status, 'resolved', $` + strconv.Itoa(len(args)+1) + `, $` + strconv.Itoa(len(args)+2) + `
			  FROM updated
		)
		SELECT COUNT(*) FROM updated`
	args = append(args, changedBy, comment)
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&changed); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return changed, err
	}
	return changed, nil
}
