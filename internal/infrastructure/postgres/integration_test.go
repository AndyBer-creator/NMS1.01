package postgres

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
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

func TestIntegration_IncidentsLifecycle(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)
	d := &domain.Device{IP: ip, Name: "incident-dev", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	item, err := repo.CreateIncident(&domain.Incident{
		DeviceID: &d.ID,
		Title:    "port down",
		Severity: "critical",
		Source:   "trap",
	})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if item == nil || item.ID <= 0 || item.Status != "new" {
		t.Fatalf("unexpected incident: %+v", item)
	}

	item, err = repo.TransitionIncidentStatus(item.ID, "acknowledged", "itest", "ack")
	if err != nil {
		t.Fatalf("Transition to acknowledged: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(item.ID, "in_progress", "itest", "work")
	if err != nil {
		t.Fatalf("Transition to in_progress: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(item.ID, "resolved", "itest", "fixed")
	if err != nil {
		t.Fatalf("Transition to resolved: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(item.ID, "closed", "itest", "done")
	if err != nil {
		t.Fatalf("Transition to closed: %v", err)
	}
	if item.Status != "closed" {
		t.Fatalf("expected closed status, got %q", item.Status)
	}

	items, err := repo.ListIncidents(50, &d.ID, "closed", "critical")
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one incident in list")
	}

	trs, err := repo.ListIncidentTransitions(item.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidentTransitions: %v", err)
	}
	if len(trs) < 4 {
		t.Fatalf("expected >=4 transitions, got %d", len(trs))
	}
}

func TestIntegration_IncidentDedupAndAutoResolve(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(ip)
	d := &domain.Device{IP: ip, Name: "incident-dedup", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	details, _ := json.Marshal(map[string]any{"status": "failed_timeout"})
	first, created, err := repo.CreateOrTouchOpenIncident(&d.ID, "SNMP device unavailable", "critical", "polling", details, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident first: %v", err)
	}
	if !created || first == nil {
		t.Fatalf("expected first incident created, got created=%v first=%+v", created, first)
	}
	second, created2, err := repo.CreateOrTouchOpenIncident(&d.ID, "SNMP device unavailable", "critical", "polling", details, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident second: %v", err)
	}
	if created2 {
		t.Fatal("expected second incident to be deduplicated/touched, not created")
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("expected same incident id on dedup, first=%+v second=%+v", first, second)
	}

	resolvedN, err := repo.ResolveOpenIncidentsBySource(d.ID, "polling", "itest", "auto")
	if err != nil {
		t.Fatalf("ResolveOpenIncidentsBySource: %v", err)
	}
	if resolvedN < 1 {
		t.Fatalf("expected at least one resolved incident, got %d", resolvedN)
	}
	item, err := repo.GetIncidentByID(first.ID)
	if err != nil {
		t.Fatalf("GetIncidentByID: %v", err)
	}
	if item == nil || item.Status != "resolved" {
		t.Fatalf("expected incident resolved, got %+v", item)
	}
}
