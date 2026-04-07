package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// LldpLink — сырые данные ребра (сопоставление/лейблы делаем в endpoint).
type LldpLink struct {
	LocalDeviceIP   string
	LocalPortNum    int
	LocalPortDesc  string
	RemoteDeviceIP  *string
	RemoteSysName   string
	RemoteSysDesc   string
	RemotePortID    string
	RemotePortDesc  string
}

type LldpLinkView struct {
	LocalIP         string
	LocalName       string
	LocalPortNum    int
	LocalPortDesc   string

	RemoteIP        *string
	RemoteName      string
	RemoteSysName   string
	RemoteSysDesc   string
	RemotePortID    string
	RemotePortDesc  string
}

func (r *Repo) CreateLldpScan() (int64, error) {
	var id int64
	err := r.db.QueryRowContext(context.Background(),
		`INSERT INTO lldp_topology_scans DEFAULT VALUES RETURNING id`).Scan(&id)
	return id, err
}

// InsertLldpLink возвращает число реально вставленных строк (0 если конфликт по UNIQUE).
func (r *Repo) InsertLldpLink(scanID int64, link LldpLink) (int64, error) {
	// remote_device_ip может быть NULL — для этого используем *string.
	res, err := r.db.ExecContext(context.Background(),
		`INSERT INTO lldp_links
			(scan_id, local_device_ip, local_port_num, local_port_desc,
			 remote_device_ip, remote_sys_name, remote_sys_desc,
			 remote_port_id, remote_port_desc)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT DO NOTHING`,
		scanID,
		link.LocalDeviceIP,
		link.LocalPortNum,
		link.LocalPortDesc,
		link.RemoteDeviceIP,
		link.RemoteSysName,
		link.RemoteSysDesc,
		link.RemotePortID,
		link.RemotePortDesc,
	)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func (r *Repo) GetLatestLldpLinks() ([]LldpLinkView, error) {
	var scanID int64
	err := r.db.QueryRowContext(context.Background(),
		`SELECT id FROM lldp_topology_scans ORDER BY id DESC LIMIT 1`).Scan(&scanID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(context.Background(), fmt.Sprintf(`
		SELECT
			l.local_device_ip::text AS local_ip,
			COALESCE(ld.name, '') AS local_name,
			COALESCE(l.local_port_num, 0) AS local_port_num,
			COALESCE(l.local_port_desc, '') AS local_port_desc,
			l.remote_device_ip::text AS remote_ip,
			COALESCE(rd.name, '') AS remote_name,
			COALESCE(l.remote_sys_name, '') AS remote_sys_name,
			COALESCE(l.remote_sys_desc, '') AS remote_sys_desc,
			COALESCE(l.remote_port_id, '') AS remote_port_id,
			COALESCE(l.remote_port_desc, '') AS remote_port_desc
		FROM lldp_links l
		LEFT JOIN devices ld ON ld.ip = l.local_device_ip
		LEFT JOIN devices rd ON rd.ip = l.remote_device_ip
		WHERE l.scan_id = %d
	`, scanID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LldpLinkView
	for rows.Next() {
		var remoteIP sql.NullString
		item := LldpLinkView{}
		if err := rows.Scan(
			&item.LocalIP,
			&item.LocalName,
			&item.LocalPortNum,
			&item.LocalPortDesc,
			&remoteIP,
			&item.RemoteName,
			&item.RemoteSysName,
			&item.RemoteSysDesc,
			&item.RemotePortID,
			&item.RemotePortDesc,
		); err != nil {
			return nil, err
		}
		if remoteIP.Valid {
			item.RemoteIP = &remoteIP.String
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) GetLatestLldpScanID() (int64, error) {
	var scanID int64
	err := r.db.QueryRowContext(context.Background(),
		`SELECT id FROM lldp_topology_scans ORDER BY id DESC LIMIT 1`).Scan(&scanID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return scanID, err
}

