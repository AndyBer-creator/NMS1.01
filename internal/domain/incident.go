package domain

import (
	"time"
)

// JSONPayload stores raw JSON bytes without transport-specific dependencies.
type JSONPayload []byte

// MarshalJSON emits the payload as-is.
func (p JSONPayload) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	return p, nil
}

// UnmarshalJSON copies raw JSON bytes into payload.
func (p *JSONPayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return nil
	}
	*p = append((*p)[:0], data...)
	return nil
}

type Incident struct {
	ID             int64           `json:"id"`
	DeviceID       *int            `json:"device_id,omitempty"`
	Assignee       *string         `json:"assignee,omitempty"`
	Title          string          `json:"title"`
	Severity       string          `json:"severity"`
	Status         string          `json:"status"`
	Source         string          `json:"source"`
	Details        JSONPayload     `json:"details,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
	ClosedAt       *time.Time      `json:"closed_at,omitempty"`
}

type IncidentTransition struct {
	ID         int64     `json:"id"`
	IncidentID int64     `json:"incident_id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	ChangedBy  string    `json:"changed_by"`
	Comment    string    `json:"comment,omitempty"`
	ChangedAt  time.Time `json:"changed_at"`
}
