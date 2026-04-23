package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	// required blank import for pgx stdlib driver registration
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Repo provides PostgreSQL-backed persistence for core NMS entities.
type Repo struct {
	db        *sql.DB
	protector *secretProtector
	ownDB     bool
}

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

type deviceSecretSnapshot struct {
	communityPlain string
	communityEnc   sql.NullString
	authPassPlain  string
	authPassEnc    sql.NullString
	privPassPlain  string
	privPassEnc    sql.NullString
}

// New opens PostgreSQL connections and initializes secret protection helpers.
func New(dsn string) (*Repo, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	applyDefaultPoolSettings(db)
	protector, err := newSecretProtectorFromEnv()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Repo{db: db, protector: protector, ownDB: true}, nil
}

// NewFromDB wires repo to an existing SQL connection pool.
func NewFromDB(db *sql.DB) (*Repo, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	applyDefaultPoolSettings(db)
	protector, err := newSecretProtectorFromEnv()
	if err != nil {
		return nil, err
	}
	return &Repo{db: db, protector: protector, ownDB: false}, nil
}

func (r *Repo) Close() error {
	if r == nil || r.db == nil || !r.ownDB {
		return nil
	}
	return r.db.Close()
}

func applyDefaultPoolSettings(db *sql.DB) {
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
}

// Ping checks database availability for readiness probes.
func (r *Repo) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// GetDeviceByID loads a device by stable numeric identifier.
func (r *Repo) GetDeviceByID(ctx context.Context, id int) (*domain.Device, error) {
	if id <= 0 {
		return nil, nil
	}
	device := &domain.Device{}
	var lastSeenSql sql.NullTime
	var lastErrorAt sql.NullTime
	var lastPollOKAt sql.NullTime

	query := `
        SELECT id, ip, name, COALESCE(community, ''),
               community_enc,
               COALESCE(version, 'unknown'),
               COALESCE(snmp_version, 'v2c'),
               COALESCE(auth_proto, ''), COALESCE(auth_pass, ''), auth_pass_enc,
               COALESCE(priv_proto, ''), COALESCE(priv_pass, ''), priv_pass_enc,
               COALESCE(status, 'active'),
               COALESCE(created_at, NOW()),
               COALESCE(last_seen, created_at),
               COALESCE(last_error, ''),
               last_error_at,
               last_poll_ok_at
        FROM devices WHERE id = $1`

	var secrets deviceSecretSnapshot
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&device.ID,
		&device.IP,
		&device.Name,
		&secrets.communityPlain,
		&secrets.communityEnc,
		&device.Version,
		&device.SNMPVersion,
		&device.AuthProto,
		&secrets.authPassPlain,
		&secrets.authPassEnc,
		&device.PrivProto,
		&secrets.privPassPlain,
		&secrets.privPassEnc,
		&device.Status,
		&device.CreatedAt,
		&lastSeenSql,
		&device.LastError,
		&lastErrorAt,
		&lastPollOKAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	device.Community, err = r.protector.mergeSecretFromStorage(secrets.communityPlain, secrets.communityEnc)
	if err != nil {
		return nil, err
	}
	device.AuthPass, err = r.protector.mergeSecretFromStorage(secrets.authPassPlain, secrets.authPassEnc)
	if err != nil {
		return nil, err
	}
	device.PrivPass, err = r.protector.mergeSecretFromStorage(secrets.privPassPlain, secrets.privPassEnc)
	if err != nil {
		return nil, err
	}
	if err := r.migrateDeviceSecretsIfNeeded(ctx, device.ID, secrets); err != nil {
		return nil, err
	}
	if lastSeenSql.Valid {
		device.LastSeen = lastSeenSql.Time
	}
	if lastErrorAt.Valid {
		device.LastErrorAt = lastErrorAt.Time
	}
	if lastPollOKAt.Valid {
		device.LastPollOKAt = lastPollOKAt.Time
	}
	return device, err
}

func (r *Repo) ListDevices(ctx context.Context) ([]*domain.Device, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id,
                 ip,
                 name,
                 COALESCE(community, ''),
                 community_enc,
                 COALESCE(version, 'unknown'),
                 COALESCE(snmp_version, 'v2c'),
                 COALESCE(auth_proto, '') as auth_proto,
                 COALESCE(auth_pass, '') as auth_pass,
                 auth_pass_enc,
                 COALESCE(priv_proto, '') as priv_proto,
                 COALESCE(priv_pass, '') as priv_pass,
                 priv_pass_enc,
                 COALESCE(status, 'active') as status,
                 COALESCE(created_at, NOW()) as created_at,
                 COALESCE(last_seen, created_at) as last_seen,
                 COALESCE(last_error, '') as last_error,
                 last_error_at,
                 last_poll_ok_at
         FROM devices ORDER BY ip`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var devices []*domain.Device
	for rows.Next() {
		device := &domain.Device{}
		var lastErrorAt sql.NullTime
		var lastPollOKAt sql.NullTime
		var communityPlain, authPassPlain, privPassPlain string
		var communityEnc, authPassEnc, privPassEnc sql.NullString
		if err := rows.Scan(
			&device.ID,
			&device.IP,
			&device.Name,
			&communityPlain,
			&communityEnc,
			&device.Version,
			&device.SNMPVersion,
			&device.AuthProto,
			&authPassPlain,
			&authPassEnc,
			&device.PrivProto,
			&privPassPlain,
			&privPassEnc,
			&device.Status,
			&device.CreatedAt,
			&device.LastSeen,
			&device.LastError,
			&lastErrorAt,
			&lastPollOKAt,
		); err != nil {
			return nil, err
		}
		device.Community, err = r.protector.mergeSecretFromStorage(communityPlain, communityEnc)
		if err != nil {
			return nil, err
		}
		device.AuthPass, err = r.protector.mergeSecretFromStorage(authPassPlain, authPassEnc)
		if err != nil {
			return nil, err
		}
		device.PrivPass, err = r.protector.mergeSecretFromStorage(privPassPlain, privPassEnc)
		if err != nil {
			return nil, err
		}
		if lastErrorAt.Valid {
			device.LastErrorAt = lastErrorAt.Time
		}
		if lastPollOKAt.Valid {
			device.LastPollOKAt = lastPollOKAt.Time
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return devices, nil
}

func (r *Repo) migrateDeviceSecretsIfNeeded(ctx context.Context, deviceID int, s deviceSecretSnapshot) error {
	if !r.protector.enabled || deviceID <= 0 {
		return nil
	}
	needsCommunity := strings.TrimSpace(s.communityPlain) != "" && !s.communityEnc.Valid
	needsAuth := strings.TrimSpace(s.authPassPlain) != "" && !s.authPassEnc.Valid
	needsPriv := strings.TrimSpace(s.privPassPlain) != "" && !s.privPassEnc.Valid
	if !needsCommunity && !needsAuth && !needsPriv {
		return nil
	}
	communityPlain, communityEnc, err := r.protector.splitSecretForStorage(s.communityPlain)
	if err != nil {
		return err
	}
	authPlain, authEnc, err := r.protector.splitSecretForStorage(s.authPassPlain)
	if err != nil {
		return err
	}
	privPlain, privEnc, err := r.protector.splitSecretForStorage(s.privPassPlain)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE devices
		SET community = $1,
		    community_enc = $2,
		    auth_pass = $3,
		    auth_pass_enc = $4,
		    priv_pass = $5,
		    priv_pass_enc = $6
		WHERE id = $7`,
		communityPlain,
		communityEnc,
		authPlain,
		authEnc,
		privPlain,
		privEnc,
		deviceID,
	)
	return err
}

func (r *Repo) DeleteByID(ctx context.Context, id int) error {
	if id <= 0 {
		return sql.ErrNoRows
	}
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM devices WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repo) UpdateDeviceByID(ctx context.Context, id int, patch *domain.Device) (*domain.Device, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id is required")
	}
	if patch == nil {
		return nil, fmt.Errorf("patch is required")
	}
	snmpVer, err := domain.NormalizeSNMPVersion(patch.SNMPVersion)
	if err != nil {
		return nil, err
	}
	authProto := sql.NullString{Valid: strings.TrimSpace(patch.AuthProto) != ""}
	if authProto.Valid {
		authProto.String = strings.TrimSpace(patch.AuthProto)
	}
	authPassPlain, authPassEnc, err := r.protector.splitSecretForStorage(patch.AuthPass)
	if err != nil {
		return nil, err
	}
	privProto := sql.NullString{Valid: strings.TrimSpace(patch.PrivProto) != ""}
	if privProto.Valid {
		privProto.String = strings.TrimSpace(patch.PrivProto)
	}
	privPassPlain, privPassEnc, err := r.protector.splitSecretForStorage(patch.PrivPass)
	if err != nil {
		return nil, err
	}
	communityPlain, communityEnc, err := r.protector.splitSecretForStorage(patch.Community)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE devices
		SET name = $1,
		    community = $2,
		    community_enc = $3,
		    snmp_version = $4,
		    auth_proto = $5,
		    auth_pass = $6,
		    auth_pass_enc = $7,
		    priv_proto = $8,
		    priv_pass = $9,
		    priv_pass_enc = $10
		WHERE id = $11`,
		strings.TrimSpace(patch.Name),
		communityPlain,
		communityEnc,
		snmpVer,
		authProto,
		authPassPlain,
		authPassEnc,
		privProto,
		privPassPlain,
		privPassEnc,
		id,
	)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetDeviceByID(ctx, id)
}

func (r *Repo) SaveMetric(ctx context.Context, deviceID int, oid, value string) error {
	if _, err := r.db.ExecContext(ctx, `SELECT ensure_metrics_partition_for(NOW())`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO metrics (device_id, oid, value) VALUES ($1, $2, $3)`,
		deviceID, oid, value)
	return err
}

func (r *Repo) PruneOldMetricPartitions(ctx context.Context, retainMonths int) (int, error) {
	if retainMonths < 1 {
		return 0, fmt.Errorf("retainMonths must be >= 1")
	}
	var dropped int
	if err := r.db.QueryRowContext(ctx, `SELECT prune_old_metrics_partitions($1)`, retainMonths).Scan(&dropped); err != nil {
		return 0, err
	}
	return dropped, nil
}

func (r *Repo) CreateDevice(ctx context.Context, device *domain.Device) error {
	snmpVer, err := domain.NormalizeSNMPVersion(device.SNMPVersion)
	if err != nil {
		return err
	}
	device.SNMPVersion = snmpVer

	// For v3 credentials store NULL instead of empty strings.
	authProto := sql.NullString{Valid: strings.TrimSpace(device.AuthProto) != ""}
	if authProto.Valid {
		authProto.String = device.AuthProto
	}
	authPassPlain, authPassEnc, err := r.protector.splitSecretForStorage(device.AuthPass)
	if err != nil {
		return err
	}
	privProto := sql.NullString{Valid: strings.TrimSpace(device.PrivProto) != ""}
	if privProto.Valid {
		privProto.String = device.PrivProto
	}
	privPassPlain, privPassEnc, err := r.protector.splitSecretForStorage(device.PrivPass)
	if err != nil {
		return err
	}
	communityPlain, communityEnc, err := r.protector.splitSecretForStorage(device.Community)
	if err != nil {
		return err
	}

	query := `
        INSERT INTO devices (ip, name, community, community_enc, snmp_version, auth_proto, auth_pass, auth_pass_enc, priv_proto, priv_pass, priv_pass_enc, status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'active')
        RETURNING id, created_at`

	return r.db.QueryRowContext(ctx, query,
		device.IP, device.Name, communityPlain, communityEnc, snmpVer, authProto, authPassPlain, authPassEnc, privProto, privPassPlain, privPassEnc).
		Scan(&device.ID, &device.CreatedAt)
}

func (r *Repo) UpdateDeviceLastSeen(deviceID int) error {
	_, err := r.db.Exec("UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID)
	return err
}

func (r *Repo) UpdateDeviceStatus(deviceID int, status string) error {
	_, err := r.db.Exec("UPDATE devices SET status = $1, last_seen = NOW() WHERE id = $2", status, deviceID)
	return err
}

func (r *Repo) MarkDevicePollSuccess(deviceID int) error {
	_, err := r.db.Exec(
		`UPDATE devices
         SET status = 'active',
             last_seen = NOW(),
             last_poll_ok_at = NOW(),
             last_error = NULL,
             last_error_at = NULL
         WHERE id = $1`,
		deviceID,
	)
	return err
}

func (r *Repo) UpdateDeviceError(deviceID int, status, errText string) error {
	if strings.TrimSpace(status) == "" {
		status = "failed"
	}
	_, err := r.db.Exec(
		`UPDATE devices
         SET status = $1,
             last_seen = NOW(),
             last_error = $2,
             last_error_at = NOW()
         WHERE id = $3`,
		status, strings.TrimSpace(errText), deviceID,
	)
	return err
}

func (r *Repo) InsertSNMPSetAudit(ctx context.Context, a SNMPSetAuditRecord) error {
	if strings.TrimSpace(a.OID) == "" {
		return fmt.Errorf("snmp set audit: oid is required")
	}
	if strings.TrimSpace(a.Result) == "" {
		a.Result = "unknown"
	}
	_, err := r.db.ExecContext(
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
