package postgres

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"NMS1/internal/domain"
	"NMS1/internal/testdb"

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
	testdb.PingDBOrSkip(t, db, 5*time.Second)
	return repo, db
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
