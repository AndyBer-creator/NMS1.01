package domain

import "time"

type Trap struct {
	ID         int       `json:"id"`
	DeviceIP   string    `json:"device_ip"`
	OID        string    `json:"oid"`
	Uptime     int64     `json:"uptime"`
	TrapVars   []byte    `json:"trap_vars"` // JSONB
	ReceivedAt time.Time `json:"received_at"`
}
