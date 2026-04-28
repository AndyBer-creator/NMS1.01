package http

import (
	"testing"
	"time"

	"NMS1/internal/domain"
)

func TestDevicesTableViewModelFromDevices_ActiveFallbackLastPollOK(t *testing.T) {
	t.Parallel()
	seen := time.Date(2026, 4, 28, 14, 19, 0, 0, time.UTC)
	vm := devicesTableViewModelFromDevices([]*domain.Device{
		{
			ID:          1,
			IP:          "10.0.0.1",
			Name:        "sw-core",
			Status:      "active",
			LastSeen:    seen,
			LastPollOKAt: time.Time{},
		},
	})
	if len(vm.Devices) != 1 {
		t.Fatalf("expected 1 device row, got %d", len(vm.Devices))
	}
	if vm.Devices[0].LastPollOK != "14:19 28.04" {
		t.Fatalf("expected last_poll fallback to last_seen, got %q", vm.Devices[0].LastPollOK)
	}
}

