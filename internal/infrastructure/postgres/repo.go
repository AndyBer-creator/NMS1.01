package postgres

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
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

	query := `
        SELECT id, ip, name, community,
               COALESCE(version, 'unknown'),
               COALESCE(snmp_version, 'v2c'),
               COALESCE(auth_proto, ''),
               COALESCE(auth_pass, ''),
               COALESCE(priv_proto, ''),
               COALESCE(priv_pass, ''),
               COALESCE(status, 'active'), COALESCE(created_at, NOW()), COALESCE(last_seen, created_at)
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
		&lastSeenSql)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if lastSeenSql.Valid {
		device.LastSeen = lastSeenSql.Time
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
                 COALESCE(last_seen, created_at) as last_seen
         FROM devices ORDER BY ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*domain.Device
	for rows.Next() {
		device := &domain.Device{}
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
		); err != nil {
			return nil, err
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
	snmpVer := device.SNMPVersion
	if snmpVer == "" {
		snmpVer = "v2c"
	}

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

func (r *Repo) UpdateDeviceLastSeen(deviceID int) error {
	_, err := r.db.Exec("UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID)
	return err
}

func (r *Repo) UpdateDeviceStatus(deviceID int, status string) error {
	_, err := r.db.Exec("UPDATE devices SET status = $1, last_seen = NOW() WHERE id = $2", status, deviceID)
	return err
}
