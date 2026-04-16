package repository

import (
	"NMS1/internal/domain"
	"context"
)

type DeviceRepository interface {
	CreateDevice(ctx context.Context, device *domain.Device) error
	GetDeviceByIP(ctx context.Context, ip string) (*domain.Device, error)
	ListDevices(ctx context.Context) ([]*domain.Device, error)
	DeleteByIP(ctx context.Context, ip string) error
	UpdateDeviceLastSeen(deviceID int) error
}

type MetricRepository interface {
	SaveMetric(ctx context.Context, deviceID int, oid, value string) error
	PruneOldMetricPartitions(ctx context.Context, retainMonths int) (int, error)
}
