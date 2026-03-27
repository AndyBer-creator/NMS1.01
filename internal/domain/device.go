package domain

import "time"

type Device struct {
	ID          int       `json:"id" db:"id"`
	IP          string    `json:"ip" db:"ip"`
	Name        string    `json:"name" db:"name"`
	Community   string    `json:"community" db:"community"`
	SNMPVersion string    `json:"snmp_version" db:"snmp_version"`
	AuthProto   string    `json:"auth_proto,omitempty" db:"auth_proto"`
	AuthPass    string    `json:"-" db:"auth_pass"` // Скрыто в JSON
	PrivProto   string    `json:"priv_proto,omitempty" db:"priv_proto"`
	PrivPass    string    `json:"-" db:"priv_pass"` // Скрыто в JSON
	Version     string    `json:"version" db:"version"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	LastSeen    time.Time `json:"last_seen" db:"last_seen"`
}

type Metric struct {
	ID        int       `json:"id"`
	DeviceID  int       `json:"device_id"`
	OID       string    `json:"oid"`
	Value     string    `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}
