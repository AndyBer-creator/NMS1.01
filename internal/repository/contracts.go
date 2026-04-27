package repository

import (
	"NMS1/internal/domain"
	"context"
	"database/sql"
	"time"
)

// TrapQueryRepository captures trap read operations used by delivery layers.
type TrapQueryRepository interface {
	List(ctx context.Context, limit int) ([]domain.Trap, error)
	ByDevice(ctx context.Context, ip string, limit int) ([]domain.Trap, error)
}

// TrapIncidentRepository captures trap-to-incident correlation operations.
type TrapIncidentRepository interface {
	Insert(ctx context.Context, deviceIP, oid string, uptime int64, trapVars map[string]string, isCritical bool) error
	CreateOrTouchOpenTrapIncident(ctx context.Context, deviceIP, oid string, trapVars map[string]string, suppressionWindow time.Duration) error
	ResolveOpenTrapIncidents(ctx context.Context, deviceID sql.NullInt64, titles []string, changedBy, comment string) (int64, error)
}

// TrapOIDMappingRepository captures CRUD operations for trap OID mappings.
type TrapOIDMappingRepository interface {
	ListOIDMappings(ctx context.Context, vendor string, enabled *bool) ([]domain.TrapOIDMapping, error)
	CreateOIDMapping(ctx context.Context, in *domain.TrapOIDMapping) (*domain.TrapOIDMapping, error)
	UpdateOIDMapping(ctx context.Context, id int64, in *domain.TrapOIDMapping) (*domain.TrapOIDMapping, error)
	DeleteOIDMapping(ctx context.Context, id int64) (bool, error)
}

// TrapRepository keeps backward-compatible full trap contract composition.
type TrapRepository interface {
	TrapQueryRepository
	TrapIncidentRepository
	TrapOIDMappingRepository
}

// TrapHTTPRepository is the minimal contract required by HTTP trap handlers.
type TrapHTTPRepository interface {
	TrapQueryRepository
	TrapOIDMappingRepository
}
