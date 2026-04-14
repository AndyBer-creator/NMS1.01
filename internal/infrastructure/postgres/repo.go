package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	// 🚨 ОБЯЗАТЕЛЬНЫЙ BLANK IMPORT
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Repo struct {
	db        *sql.DB
	protector *secretProtector
}

type SNMPSetAuditRecord struct {
	UserName string
	DeviceID sql.NullInt64
	OID      string
	OldValue string
	NewValue string
	Result   string
	Error    string
}

func New(dsn string) (*Repo, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	protector, err := newSecretProtectorFromEnv()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Repo{db: db, protector: protector}, nil
}

func (r *Repo) Close() error {
	return r.db.Close()
}

// Ping проверяет доступность БД (readiness probe).
func (r *Repo) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// Devices
func (r *Repo) GetDeviceByIP(ip string) (*domain.Device, error) {
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
        FROM devices WHERE ip = $1`

	var communityPlain, authPassPlain, privPassPlain string
	var communityEnc, authPassEnc, privPassEnc sql.NullString
	err := r.db.QueryRowContext(context.Background(), query, ip).Scan(
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

// GetDeviceByID загружает устройство по id (стабильные URL вместо IP/IPv6 в пути).
func (r *Repo) GetDeviceByID(id int) (*domain.Device, error) {
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

	var communityPlain, authPassPlain, privPassPlain string
	var communityEnc, authPassEnc, privPassEnc sql.NullString
	err := r.db.QueryRowContext(context.Background(), query, id).Scan(
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

func (r *Repo) ListDevices() ([]*domain.Device, error) {
	rows, err := r.db.QueryContext(context.Background(),
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
	return devices, nil
}

func (r *Repo) DeleteByIP(ip string) error {
	res, err := r.db.ExecContext(context.Background(),
		`DELETE FROM devices WHERE ip = $1`, ip)
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

func (r *Repo) DeleteByID(id int) error {
	if id <= 0 {
		return sql.ErrNoRows
	}
	res, err := r.db.ExecContext(context.Background(),
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

func (r *Repo) UpdateDeviceByIP(ip string, patch *domain.Device) (*domain.Device, error) {
	if strings.TrimSpace(ip) == "" {
		return nil, fmt.Errorf("ip is required")
	}
	if patch == nil {
		return nil, fmt.Errorf("patch is required")
	}

	// Нормализуем snmp_version как в CreateDevice.
	snmpVer, err := normalizeSNMPVersion(patch.SNMPVersion)
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

	_, err = r.db.ExecContext(context.Background(), `
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
		WHERE ip = $11`,
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
		ip,
	)
	if err != nil {
		return nil, err
	}
	return r.GetDeviceByIP(ip)
}

func (r *Repo) UpdateDeviceByID(id int, patch *domain.Device) (*domain.Device, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id is required")
	}
	if patch == nil {
		return nil, fmt.Errorf("patch is required")
	}
	snmpVer, err := normalizeSNMPVersion(patch.SNMPVersion)
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
	_, err = r.db.ExecContext(context.Background(), `
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
	return r.GetDeviceByID(id)
}

func (r *Repo) SaveMetric(deviceID int, oid, value string) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO metrics (device_id, oid, value) VALUES ($1, $2, $3)`,
		deviceID, oid, value)
	return err
}

func (r *Repo) CreateDevice(device *domain.Device) error {
	snmpVer, err := normalizeSNMPVersion(device.SNMPVersion)
	if err != nil {
		return err
	}
	device.SNMPVersion = snmpVer

	// Для v3 credentials храним NULL вместо пустых строк.
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

	return r.db.QueryRowContext(context.Background(), query,
		device.IP, device.Name, communityPlain, communityEnc, snmpVer, authProto, authPassPlain, authPassEnc, privProto, privPassPlain, privPassEnc).
		Scan(&device.ID, &device.CreatedAt)
}

// normalizeSNMPVersion приводит значение к допустимым для chk_snmp_version: v1, v2c, v3.
func normalizeSNMPVersion(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "v1":
		return "v1", nil
	case "v3":
		return "v3", nil
	case "2c", "v2c", "":
		return "v2c", nil
	default:
		return "", fmt.Errorf("invalid snmp_version %q (allowed: v1, v2c, v3)", v)
	}
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

func (r *Repo) InsertSNMPSetAudit(a SNMPSetAuditRecord) error {
	if strings.TrimSpace(a.OID) == "" {
		return fmt.Errorf("snmp set audit: oid is required")
	}
	if strings.TrimSpace(a.Result) == "" {
		a.Result = "unknown"
	}
	_, err := r.db.ExecContext(
		context.Background(),
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
