package domain

import "time"

type TrapOIDMapping struct {
	ID         int64     `json:"id"`
	Vendor     string    `json:"vendor"`
	OIDPattern string    `json:"oid_pattern"`
	SignalKind string    `json:"signal_kind"`
	Title      string    `json:"title"`
	Severity   string    `json:"severity"`
	IsRecovery bool      `json:"is_recovery"`
	Priority   int       `json:"priority"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}
