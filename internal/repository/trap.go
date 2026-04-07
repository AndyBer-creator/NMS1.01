package repository

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"encoding/json"
)

type TrapsRepo struct {
	db *sql.DB
}

func NewTrapsRepo(db *sql.DB) *TrapsRepo {
	return &TrapsRepo{db: db}
}

func (r *TrapsRepo) List(ctx context.Context, limit int) ([]domain.Trap, error) {
	query := `
        SELECT id, device_ip, oid, uptime, trap_vars, received_at 
        FROM traps 
        ORDER BY received_at DESC LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	traps := make([]domain.Trap, 0)
	for rows.Next() {
		var t domain.Trap
		err := rows.Scan(&t.ID, &t.DeviceIP, &t.OID, &t.Uptime, &t.TrapVars, &t.ReceivedAt)
		if err != nil {
			return nil, err
		}
		traps = append(traps, t)
	}
	return traps, nil
}

func (r *TrapsRepo) ByDevice(ctx context.Context, ip string, limit int) ([]domain.Trap, error) {
	query := `
        SELECT id, device_ip, oid, uptime, trap_vars, received_at
        FROM traps
        WHERE device_ip = $1
        ORDER BY received_at DESC LIMIT $2`

	// ✅ 2 аргумента: ip + limit
	rows, err := r.db.QueryContext(ctx, query, ip, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	traps := make([]domain.Trap, 0)
	for rows.Next() {
		var t domain.Trap // ✅ ОДИНОЧНЫЙ Trap, НЕ слайс!
		err := rows.Scan(&t.ID, &t.DeviceIP, &t.OID, &t.Uptime, &t.TrapVars, &t.ReceivedAt)
		if err != nil {
			return nil, err
		}
		traps = append(traps, t)
	}
	return traps, nil
}

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
