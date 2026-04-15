package domain

import "time"

type ITSMInboundMapping struct {
	ID               int64     `json:"id"`
	Provider         string    `json:"provider"`
	ExternalStatus   string    `json:"external_status"`
	ExternalPriority string    `json:"external_priority"`
	ExternalOwner    string    `json:"external_owner"`
	MappedStatus     string    `json:"mapped_status"`
	MappedAssignee   string    `json:"mapped_assignee"`
	Enabled          bool      `json:"enabled"`
	Priority         int       `json:"priority"`
	CreatedAt        time.Time `json:"created_at"`
}
