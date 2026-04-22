package repository

import (
	"NMS1/internal/domain"
	"context"
)

type DeviceRepository interface {
	CreateDevice(ctx context.Context, device *domain.Device) error
	GetDeviceByID(ctx context.Context, id int) (*domain.Device, error)
	ListDevices(ctx context.Context) ([]*domain.Device, error)
	DeleteByID(ctx context.Context, id int) error
	UpdateDeviceByID(ctx context.Context, id int, patch *domain.Device) (*domain.Device, error)
	UpdateDeviceLastSeen(deviceID int) error
}

type MetricRepository interface {
	SaveMetric(ctx context.Context, deviceID int, oid, value string) error
	PruneOldMetricPartitions(ctx context.Context, retainMonths int) (int, error)
}
