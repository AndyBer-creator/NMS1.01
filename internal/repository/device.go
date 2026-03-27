package repository

import (
	"NMS1/internal/domain"
	"time"
)

type DeviceRepository interface {
	Create(device *domain.Device) error
	GetByIP(ip string) (*domain.Device, error)
	List() ([]*domain.Device, error)
	DeleteByIP(ip string) error
	UpdateLastSeen(id int, lastSeen time.Time) error
}

type MetricRepository interface {
	Save(deviceID int, oid, value string) error
	GetLatest(deviceID int, oid string) (string, error)
}
