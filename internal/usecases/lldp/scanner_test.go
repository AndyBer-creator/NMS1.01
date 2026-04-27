package lldp

import (
	"context"
	"errors"
	"testing"
	"time"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

func TestNormalizeKey(t *testing.T) {
	if got := normalizeKey("  Switch-A  "); got != "switch-a" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeOID(t *testing.T) {
	if got := normalizeOID("  .1.2.3.4 "); got != "1.2.3.4" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSingleIndexFromWalk(t *testing.T) {
	n, ok := parseSingleIndexFromWalk(lldpLocPortDescBase+".7", lldpLocPortDescBase)
	if !ok || n != 7 {
		t.Fatalf("got %d ok=%v", n, ok)
	}
	if _, ok := parseSingleIndexFromWalk("1.2.3", lldpLocPortDescBase); ok {
		t.Fatal("unrelated OID must fail")
	}
}

func TestParseRemoteIndexes(t *testing.T) {
	local, rem, ok := parseRemoteIndexes(lldpRemSysNameBase+".0.5.2", lldpRemSysNameBase)
	if !ok || local != 5 || rem != 2 {
		t.Fatalf("got local=%d rem=%d ok=%v", local, rem, ok)
	}
	if _, _, ok := parseRemoteIndexes(lldpRemSysNameBase+".0.5", lldpRemSysNameBase); ok {
		t.Fatal("short suffix must fail")
	}
}

func TestLLDPDefaults(t *testing.T) {
	t.Setenv("NMS_LLDP_DEVICE_WALK_TIMEOUT", "")
	t.Setenv("NMS_LLDP_MAX_REMOTE_ENTRIES", "")
	if got := lldpDeviceWalkTimeout(); got != defaultLLDPDeviceWalkTimeout {
		t.Fatalf("unexpected default walk timeout: %v", got)
	}
	if got := lldpMaxRemoteEntries(); got != defaultLLDPMaxRemoteEntries {
		t.Fatalf("unexpected default remote entries cap: %d", got)
	}
}

func TestWalkWithTimeout_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := walkWithTimeout(ctx, 200*time.Millisecond, func() (map[string]string, error) {
		time.Sleep(50 * time.Millisecond)
		return map[string]string{}, nil
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

type lldpRepoMock struct {
	devices []domain.Device
	links   []postgres.LldpLink
}

func (m *lldpRepoMock) ListDevices(_ context.Context) ([]*domain.Device, error) {
	out := make([]*domain.Device, 0, len(m.devices))
	for i := range m.devices {
		d := m.devices[i]
		out = append(out, &d)
	}
	return out, nil
}

func (m *lldpRepoMock) CreateLldpScan(_ context.Context) (int64, error) { return 42, nil }
func (m *lldpRepoMock) DeleteLldpScan(_ context.Context, _ int64) error  { return nil }
func (m *lldpRepoMock) InsertLldpLink(_ context.Context, _ int64, link postgres.LldpLink) (int64, error) {
	m.links = append(m.links, link)
	return 1, nil
}

type lldpWalkerMock struct {
	data map[string]map[string]string
}

func (m *lldpWalkerMock) WalkDevice(_ *domain.Device, baseOID string) (map[string]string, error) {
	out, ok := m.data[baseOID]
	if !ok {
		return nil, errors.New("missing walk data")
	}
	return out, nil
}

func TestScanAllDevicesLLDP_WithMockSNMP(t *testing.T) {
	repo := &lldpRepoMock{
		devices: []domain.Device{
			{ID: 1, IP: "10.0.0.1", Name: "sw1"},
			{ID: 2, IP: "10.0.0.2", Name: "sw2"},
		},
	}
	walker := &lldpWalkerMock{
		data: map[string]map[string]string{
			lldpLocPortDescBase: {"1.0.8802.1.1.2.1.3.7.1.4.1": "Gi0/1"},
			lldpLocPortIdBase:   {"1.0.8802.1.1.2.1.3.7.1.3.1": "1"},
			lldpRemSysNameBase:  {"1.0.8802.1.1.2.1.4.1.1.9.0.1.1": "sw2"},
			lldpRemSysDescBase:  {"1.0.8802.1.1.2.1.4.1.1.10.0.1.1": "Switch 2"},
			lldpRemPortIdBase:   {"1.0.8802.1.1.2.1.4.1.1.7.0.1.1": "Gi0/2"},
			lldpRemPortDescBase: {"1.0.8802.1.1.2.1.4.1.1.8.0.1.1": "Uplink"},
		},
	}

	summary, err := ScanAllDevicesLLDP(context.Background(), repo, walker, zap.NewNop(), ScanParams{})
	if err != nil {
		t.Fatalf("ScanAllDevicesLLDP: %v", err)
	}
	if summary == nil || summary.ScanID != 42 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.DevicesScanned != 2 {
		t.Fatalf("expected devices scanned=2, got %d", summary.DevicesScanned)
	}
	if summary.LinksFound < 1 || summary.LinksInserted < 1 {
		t.Fatalf("expected links found/inserted > 0, got %+v", summary)
	}
	if len(repo.links) == 0 {
		t.Fatal("expected persisted links")
	}
	if repo.links[0].RemoteDeviceIP == nil || *repo.links[0].RemoteDeviceIP != "10.0.0.2" {
		t.Fatalf("expected remote device IP match from inventory, got %+v", repo.links[0].RemoteDeviceIP)
	}
}
