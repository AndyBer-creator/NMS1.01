package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"NMS1/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		t.Skip("integration: set DB_DSN to run against PostgreSQL")
	}
	return dsn
}

func uniqueInet(t *testing.T) string {
	t.Helper()
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("172.19.%d.%d", b[0], b[1])
}

func openIntegrationRepo(t *testing.T) (*Repo, *sql.DB) {
	t.Helper()
	dsn := integrationDSN(t)
	repo, err := New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		_ = repo.Close()
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
		_ = db.Close()
	})
	pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pcancel()
	if err := db.PingContext(pctx); err != nil {
		t.Skipf("integration: postgres unreachable (%v)", err)
	}
	return repo, db
}

func TestIntegration_DeviceCreateGetListDelete(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)

	d := &domain.Device{
		IP:          ip,
		Name:        "integration-device",
		Community:   "public",
		SNMPVersion: "v2c",
	}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if d.ID <= 0 {
		t.Fatal("expected positive device id")
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	got, err := repo.GetDeviceByIP(ip)
	if err != nil {
		t.Fatalf("GetDeviceByIP: %v", err)
	}
	if got == nil || got.Name != d.Name || got.Community != "public" {
		t.Fatalf("GetDeviceByIP: %+v", got)
	}

	list, err := repo.ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	found := false
	for _, x := range list {
		if x.IP == ip {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("created device not in ListDevices")
	}

	if err := repo.DeleteByIP(ip); err != nil {
		t.Fatalf("DeleteByIP: %v", err)
	}
	after, err := repo.GetDeviceByIP(ip)
	if err != nil {
		t.Fatalf("GetDeviceByIP after delete: %v", err)
	}
	if after != nil {
		t.Fatal("expected nil device after delete")
	}
}

func TestIntegration_CreateDevice_InvalidSNMPVersion(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	d := &domain.Device{IP: ip, Name: "x", Community: "c", SNMPVersion: "v9"}
	if err := repo.CreateDevice(d); err == nil {
		t.Fatal("expected error for invalid snmp_version")
	}
}

func TestIntegration_UpdateDeviceByIPAndMetrics(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)

	d := &domain.Device{IP: ip, Name: "before", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	updated, err := repo.UpdateDeviceByIP(ip, &domain.Device{Name: "after", Community: "private", SNMPVersion: "v3", AuthProto: "MD5", AuthPass: "a", PrivProto: "DES", PrivPass: "p"})
	if err != nil {
		t.Fatalf("UpdateDeviceByIP: %v", err)
	}
	if updated.Name != "after" || updated.SNMPVersion != "v3" {
		t.Fatalf("patch: %+v", updated)
	}

	oid := "1.3.6.1.2.1.1.5.0"
	val := "itest-hostname"
	if err := repo.SaveMetric(d.ID, oid, val); err != nil {
		t.Fatalf("SaveMetric: %v", err)
	}
	var got string
	err = db.QueryRowContext(context.Background(),
		`SELECT value FROM metrics WHERE device_id = $1 AND oid = $2 ORDER BY id DESC LIMIT 1`,
		d.ID, oid).Scan(&got)
	if err != nil {
		t.Fatalf("read metric: %v", err)
	}
	if got != val {
		t.Fatalf("metric value: got %q want %q", got, val)
	}
}

func TestIntegration_DeviceHealthAndAudit(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)

	d := &domain.Device{IP: ip, Name: "health", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	if err := repo.UpdateDeviceError(d.ID, "failed", "timeout"); err != nil {
		t.Fatalf("UpdateDeviceError: %v", err)
	}
	afterErr, err := repo.GetDeviceByIP(ip)
	if err != nil || afterErr == nil || afterErr.Status != "failed" || !strings.Contains(afterErr.LastError, "timeout") {
		t.Fatalf("after error: %+v err=%v", afterErr, err)
	}

	if err := repo.MarkDevicePollSuccess(d.ID); err != nil {
		t.Fatalf("MarkDevicePollSuccess: %v", err)
	}
	ok, err := repo.GetDeviceByIP(ip)
	if err != nil || ok == nil || ok.Status != "active" || ok.LastError != "" {
		t.Fatalf("after success: %+v", ok)
	}
	if ok.LastPollOKAt.IsZero() {
		t.Fatal("expected last_poll_ok_at set")
	}

	if err := repo.InsertSNMPSetAudit(SNMPSetAuditRecord{UserName: "admin", DeviceID: sql.NullInt64{Int64: int64(d.ID), Valid: true}, OID: "1.2.3", OldValue: "a", NewValue: "b", Result: "ok"}); err != nil {
		t.Fatalf("InsertSNMPSetAudit: %v", err)
	}
	if err := repo.InsertSNMPSetAudit(SNMPSetAuditRecord{OID: "", Result: "x"}); err == nil {
		t.Fatal("empty OID must fail")
	}
}

func TestIntegration_AvailabilityEvents(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)

	d := &domain.Device{IP: ip, Name: "avail", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	if err := repo.InsertAvailabilityEvent(d.ID, "unavailable", "poll failed"); err != nil {
		t.Fatalf("InsertAvailabilityEvent: %v", err)
	}
	if err := repo.InsertAvailabilityEvent(d.ID, "bogus", "x"); err != nil {
		t.Fatalf("invalid kind should return nil err: %v", err)
	}

	devID := d.ID
	events, err := repo.ListAvailabilityEvents(50, &devID)
	if err != nil {
		t.Fatalf("ListAvailabilityEvents: %v", err)
	}
	if len(events) < 1 || events[0].Kind != "unavailable" {
		t.Fatalf("events: %+v", events)
	}
}

func TestIntegration_WorkerPollSettingsRoundTrip(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	prev := repo.GetWorkerPollIntervalSeconds()
	t.Cleanup(func() { _ = repo.SetWorkerPollIntervalSeconds(prev) })

	if err := repo.SetWorkerPollIntervalSeconds(333); err != nil {
		t.Fatalf("SetWorkerPollIntervalSeconds: %v", err)
	}
	if got := repo.GetWorkerPollIntervalSeconds(); got != 333 {
		t.Fatalf("got %d want 333", got)
	}
	// clamp low
	if err := repo.SetWorkerPollIntervalSeconds(3); err != nil {
		t.Fatal(err)
	}
	if got := repo.GetWorkerPollIntervalSeconds(); got != MinWorkerPollIntervalSeconds {
		t.Fatalf("clamp low: got %d", got)
	}
}

func TestIntegration_AlertEmailSetting(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	prev := repo.GetAlertEmailTo()
	t.Cleanup(func() { _ = repo.SetAlertEmailTo(prev) })

	if err := repo.SetAlertEmailTo("  itest@example.com  "); err != nil {
		t.Fatalf("SetAlertEmailTo: %v", err)
	}
	if got := repo.GetAlertEmailTo(); got != "itest@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestIntegration_LldpScanAndLink(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	scanID, err := repo.CreateLldpScan()
	if err != nil || scanID <= 0 {
		t.Fatalf("CreateLldpScan: id=%d err=%v", scanID, err)
	}

	ip := uniqueInet(t)
	link := LldpLink{
		LocalDeviceIP:  ip,
		LocalPortNum:   1,
		LocalPortDesc:  "eth0",
		RemoteSysName:  "remote-sw",
		RemoteSysDesc:  "desc",
		RemotePortID:   "1/1",
		RemotePortDesc: "uplink",
	}
	n, err := repo.InsertLldpLink(scanID, link)
	if err != nil {
		t.Fatalf("InsertLldpLink: %v", err)
	}
	if n != 1 {
		t.Fatalf("inserted rows: %d", n)
	}
}

func TestIntegration_DeleteByIP_NotFound(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	// TEST-NET-1 (RFC 5737): не пересекается с uniqueInet 172.19.0.0/16 в этих тестах.
	if err := repo.DeleteByIP("192.0.2.199"); err != sql.ErrNoRows {
		t.Fatalf("want sql.ErrNoRows, got %v", err)
	}
}
