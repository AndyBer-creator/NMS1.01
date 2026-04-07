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
	"go.uber.org/zap"
)

type Repo struct {
	db     *sql.DB
	logger *zap.Logger
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
	return &Repo{db: db}, nil
}

func (r *Repo) Close() error {
	return r.db.Close()
}

// Devices
func (r *Repo) GetDeviceByIP(ip string) (*domain.Device, error) {
	device := &domain.Device{}
	var lastSeenSql sql.NullTime
	var lastErrorAt sql.NullTime
	var lastPollOKAt sql.NullTime

	query := `
        SELECT id, ip, name, community,
               COALESCE(version, 'unknown'),
               COALESCE(snmp_version, 'v2c'),
               COALESCE(auth_proto, ''),
               COALESCE(auth_pass, ''),
               COALESCE(priv_proto, ''),
               COALESCE(priv_pass, ''),
               COALESCE(status, 'active'),
               COALESCE(created_at, NOW()),
               COALESCE(last_seen, created_at),
               COALESCE(last_error, ''),
               last_error_at,
               last_poll_ok_at
        FROM devices WHERE ip = $1`

	err := r.db.QueryRowContext(context.Background(), query, ip).Scan(
		&device.ID,
		&device.IP,
		&device.Name,
		&device.Community,
		&device.Version,
		&device.SNMPVersion,
		&device.AuthProto,
		&device.AuthPass,
		&device.PrivProto,
		&device.PrivPass,
		&device.Status,
		&device.CreatedAt,
		&lastSeenSql,
		&device.LastError,
		&lastErrorAt,
		&lastPollOKAt)

	if err == sql.ErrNoRows {
		return nil, nil
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
                 COALESCE(community, 'public'),
                 COALESCE(version, 'unknown'),
                 COALESCE(snmp_version, 'v2c'),
                 COALESCE(auth_proto, '') as auth_proto,
                 COALESCE(auth_pass, '') as auth_pass,
                 COALESCE(priv_proto, '') as priv_proto,
                 COALESCE(priv_pass, '') as priv_pass,
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
	defer rows.Close()

	var devices []*domain.Device
	for rows.Next() {
		device := &domain.Device{}
		var lastErrorAt sql.NullTime
		var lastPollOKAt sql.NullTime
		if err := rows.Scan(
			&device.ID,
			&device.IP,
			&device.Name,
			&device.Community,
			&device.Version,
			&device.SNMPVersion,
			&device.AuthProto,
			&device.AuthPass,
			&device.PrivProto,
			&device.PrivPass,
			&device.Status,
			&device.CreatedAt,
			&device.LastSeen,
			&device.LastError,
			&lastErrorAt,
			&lastPollOKAt,
		); err != nil {
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
	authPass := sql.NullString{Valid: device.AuthPass != ""}
	if authPass.Valid {
		authPass.String = device.AuthPass
	}
	privProto := sql.NullString{Valid: strings.TrimSpace(device.PrivProto) != ""}
	if privProto.Valid {
		privProto.String = device.PrivProto
	}
	privPass := sql.NullString{Valid: device.PrivPass != ""}
	if privPass.Valid {
		privPass.String = device.PrivPass
	}

	query := `
        INSERT INTO devices (ip, name, community, snmp_version, auth_proto, auth_pass, priv_proto, priv_pass, status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
        RETURNING id, created_at`

	return r.db.QueryRowContext(context.Background(), query,
		device.IP, device.Name, device.Community, snmpVer, authProto, authPass, privProto, privPass).
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
