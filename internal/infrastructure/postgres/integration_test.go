package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
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

	d := &domain.Device{IP: ip, Name: "avail", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	if err := repo.InsertAvailabilityEvent(context.Background(), d.ID, "unavailable", "poll failed"); err != nil {
		t.Fatalf("InsertAvailabilityEvent: %v", err)
	}
	if err := repo.InsertAvailabilityEvent(context.Background(), d.ID, "bogus", "x"); err != nil {
		t.Fatalf("invalid kind should return nil err: %v", err)
	}

	devID := d.ID
	events, err := repo.ListAvailabilityEvents(context.Background(), 50, &devID)
	if err != nil {
		t.Fatalf("ListAvailabilityEvents: %v", err)
	}
	if len(events) < 1 || events[0].Kind != "unavailable" {
		t.Fatalf("events: %+v", events)
	}
}

func TestIntegration_WorkerPollSettingsRoundTrip(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	prev := repo.GetWorkerPollIntervalSeconds(context.Background())
	t.Cleanup(func() { _ = repo.SetWorkerPollIntervalSeconds(context.Background(), prev) })

	if err := repo.SetWorkerPollIntervalSeconds(context.Background(), 333); err != nil {
		t.Fatalf("SetWorkerPollIntervalSeconds: %v", err)
	}
	if got := repo.GetWorkerPollIntervalSeconds(context.Background()); got != 333 {
		t.Fatalf("got %d want 333", got)
	}
	// clamp low
	if err := repo.SetWorkerPollIntervalSeconds(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if got := repo.GetWorkerPollIntervalSeconds(context.Background()); got != MinWorkerPollIntervalSeconds {
		t.Fatalf("clamp low: got %d", got)
	}
}

func TestIntegration_AlertEmailSetting(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	prev := repo.GetAlertEmailTo(context.Background())
	t.Cleanup(func() { _ = repo.SetAlertEmailTo(context.Background(), prev) })

	if err := repo.SetAlertEmailTo(context.Background(), "  itest@example.com  "); err != nil {
		t.Fatalf("SetAlertEmailTo: %v", err)
	}
	if got := repo.GetAlertEmailTo(context.Background()); got != "itest@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestIntegration_SessionRevocationsRoundTrip(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	jti := fmt.Sprintf("itest-jti-%d", time.Now().UnixNano())
	revoked, err := repo.IsSessionJTIRevoked(context.Background(), jti, time.Now().Unix())
	if err != nil {
		t.Fatalf("IsSessionJTIRevoked before revoke: %v", err)
	}
	if revoked {
		t.Fatal("fresh jti must not be revoked")
	}

	expUnix := time.Now().Add(10 * time.Minute).Unix()
	if err := repo.RevokeSessionJTI(context.Background(), jti, expUnix); err != nil {
		t.Fatalf("RevokeSessionJTI: %v", err)
	}

	revoked, err = repo.IsSessionJTIRevoked(context.Background(), jti, time.Now().Unix())
	if err != nil {
		t.Fatalf("IsSessionJTIRevoked after revoke: %v", err)
	}
	if !revoked {
		t.Fatal("expected revoked jti to be visible via shared store")
	}
}

func TestIntegration_PollTransitionsAreAtomic(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	d := &domain.Device{IP: ip, Name: "poll-atomic", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	details, _ := json.Marshal(map[string]any{"status": "failed_timeout", "error": "timeout"})
	if err := repo.RecordPollFailureTransition(context.Background(), d.ID, "failed_timeout", "timeout", details, 10*time.Minute); err != nil {
		t.Fatalf("RecordPollFailureTransition: %v", err)
	}

	dev, err := repo.GetDeviceByID(context.Background(), d.ID)
	if err != nil || dev == nil {
		t.Fatalf("GetDeviceByID after failure: dev=%+v err=%v", dev, err)
	}
	if dev.Status != "failed_timeout" || !strings.Contains(dev.LastError, "timeout") {
		t.Fatalf("unexpected device after failure: %+v", dev)
	}

	var unavailableEvents int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM device_availability_events WHERE device_id = $1 AND kind = 'unavailable'`, d.ID,
	).Scan(&unavailableEvents); err != nil {
		t.Fatalf("count unavailable events: %v", err)
	}
	if unavailableEvents != 1 {
		t.Fatalf("expected 1 unavailable event, got %d", unavailableEvents)
	}

	var openIncidents int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM incidents WHERE device_id = $1 AND source = 'polling' AND status IN ('new','acknowledged','in_progress')`, d.ID,
	).Scan(&openIncidents); err != nil {
		t.Fatalf("count open incidents: %v", err)
	}
	if openIncidents != 1 {
		t.Fatalf("expected 1 open polling incident, got %d", openIncidents)
	}

	if err := repo.RecordPollRecoveryTransition(context.Background(), d.ID); err != nil {
		t.Fatalf("RecordPollRecoveryTransition: %v", err)
	}

	dev, err = repo.GetDeviceByID(context.Background(), d.ID)
	if err != nil || dev == nil {
		t.Fatalf("GetDeviceByID after recovery: dev=%+v err=%v", dev, err)
	}
	if dev.Status != "active" || dev.LastError != "" {
		t.Fatalf("unexpected device after recovery: %+v", dev)
	}
	if dev.LastPollOKAt.IsZero() {
		t.Fatal("expected last_poll_ok_at set after recovery")
	}

	var availableEvents int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM device_availability_events WHERE device_id = $1 AND kind = 'available'`, d.ID,
	).Scan(&availableEvents); err != nil {
		t.Fatalf("count available events: %v", err)
	}
	if availableEvents != 1 {
		t.Fatalf("expected 1 available event, got %d", availableEvents)
	}

	var resolvedIncidents int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM incidents WHERE device_id = $1 AND source = 'polling' AND status = 'resolved'`, d.ID,
	).Scan(&resolvedIncidents); err != nil {
		t.Fatalf("count resolved incidents: %v", err)
	}
	if resolvedIncidents != 1 {
		t.Fatalf("expected 1 resolved polling incident, got %d", resolvedIncidents)
	}
}

func TestIntegration_ApplyITSMInboundUpdate_IsAtomic(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	d := &domain.Device{IP: ip, Name: "itsm-atomic", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	item, err := repo.CreateIncident(context.Background(), &domain.Incident{
		DeviceID: &d.ID,
		Title:    "itsm atomic",
		Severity: "warning",
		Source:   "manual",
	})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}

	out, statusChanged, assigneeChanged, err := repo.ApplyITSMInboundUpdate(context.Background(), item.ID, "acknowledged", "noc-l2", "itest", "sync")
	if err != nil {
		t.Fatalf("ApplyITSMInboundUpdate: %v", err)
	}
	if out == nil {
		t.Fatal("expected updated incident")
		return
	}
	if !statusChanged || !assigneeChanged {
		t.Fatalf("expected both status and assignee changed, got statusChanged=%v assigneeChanged=%v", statusChanged, assigneeChanged)
	}
	if out.Status != "acknowledged" {
		t.Fatalf("status: got %q", out.Status)
	}
	if out.Assignee == nil || *out.Assignee != "noc-l2" {
		t.Fatalf("assignee: %+v", out.Assignee)
	}
}

func TestIntegration_SNMPRuntimeSettingsRoundTrip(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	prevTimeout := repo.GetSNMPTimeoutSeconds(context.Background(), DefaultSNMPTimeoutSeconds)
	prevRetries := repo.GetSNMPRetries(context.Background(), DefaultSNMPRetries)
	t.Cleanup(func() {
		_ = repo.SetSNMPTimeoutSeconds(context.Background(), prevTimeout)
		_ = repo.SetSNMPRetries(context.Background(), prevRetries)
	})

	if err := repo.SetSNMPTimeoutSeconds(context.Background(), 7); err != nil {
		t.Fatalf("SetSNMPTimeoutSeconds: %v", err)
	}
	if err := repo.SetSNMPRetries(context.Background(), 2); err != nil {
		t.Fatalf("SetSNMPRetries: %v", err)
	}
	if got := repo.GetSNMPTimeoutSeconds(context.Background(), DefaultSNMPTimeoutSeconds); got != 7 {
		t.Fatalf("timeout: got %d want 7", got)
	}
	if got := repo.GetSNMPRetries(context.Background(), DefaultSNMPRetries); got != 2 {
		t.Fatalf("retries: got %d want 2", got)
	}
	if err := repo.SetSNMPTimeoutSeconds(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSNMPRetries(context.Background(), -1); err != nil {
		t.Fatal(err)
	}
	if got := repo.GetSNMPTimeoutSeconds(context.Background(), DefaultSNMPTimeoutSeconds); got != MinSNMPTimeoutSeconds {
		t.Fatalf("timeout clamp low: got %d", got)
	}
	if got := repo.GetSNMPRetries(context.Background(), DefaultSNMPRetries); got != MinSNMPRetries {
		t.Fatalf("retries clamp low: got %d", got)
	}
}

func TestIntegration_LldpScanAndLink(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	scanID, err := repo.CreateLldpScan(context.Background())
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
	n, err := repo.InsertLldpLink(context.Background(), scanID, link)
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
	d := &domain.Device{IP: ip, Name: "incident-dev", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	item, err := repo.CreateIncident(context.Background(), &domain.Incident{
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

	item, err = repo.TransitionIncidentStatus(context.Background(), item.ID, "acknowledged", "itest", "ack")
	if err != nil {
		t.Fatalf("Transition to acknowledged: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(context.Background(), item.ID, "in_progress", "itest", "work")
	if err != nil {
		t.Fatalf("Transition to in_progress: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(context.Background(), item.ID, "resolved", "itest", "fixed")
	if err != nil {
		t.Fatalf("Transition to resolved: %v", err)
	}
	item, err = repo.TransitionIncidentStatus(context.Background(), item.ID, "closed", "itest", "done")
	if err != nil {
		t.Fatalf("Transition to closed: %v", err)
	}
	if item.Status != "closed" {
		t.Fatalf("expected closed status, got %q", item.Status)
	}

	items, err := repo.ListIncidents(context.Background(), 50, &d.ID, "closed", "critical")
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one incident in list")
	}

	trs, err := repo.ListIncidentTransitions(context.Background(), item.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidentTransitions: %v", err)
	}
	if len(trs) < 4 {
		t.Fatalf("expected >=4 transitions, got %d", len(trs))
	}
}

func TestIntegration_IncidentEscalationByAckTimeout(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	item, err := repo.CreateIncident(context.Background(), &domain.Incident{
		Title:    "escalation-test",
		Severity: "warning",
		Source:   "manual",
		Assignee: nil,
	})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if item == nil || item.ID <= 0 {
		t.Fatalf("unexpected incident: %+v", item)
	}
	if _, err := db.Exec(`UPDATE incidents SET created_at = NOW() - interval '20 minutes' WHERE id = $1`, item.ID); err != nil {
		t.Fatalf("force old created_at: %v", err)
	}

	changed, err := repo.EscalateUnackedIncidents(context.Background(), 10*time.Minute, "noc-escalation", "itest", "escalated-by-test")
	if err != nil {
		t.Fatalf("EscalateUnackedIncidents: %v", err)
	}
	if changed < 1 {
		t.Fatalf("expected >=1 escalated incidents, got %d", changed)
	}
	updated, err := repo.GetIncidentByID(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("GetIncidentByID: %v", err)
	}
	if updated == nil || updated.Assignee == nil || *updated.Assignee != "noc-escalation" {
		t.Fatalf("expected assignee noc-escalation, got %+v", updated)
	}
	trs, err := repo.ListIncidentTransitions(context.Background(), item.ID, 20)
	if err != nil {
		t.Fatalf("ListIncidentTransitions: %v", err)
	}
	found := false
	for _, tr := range trs {
		if tr.ChangedBy == "itest" && tr.Comment == "escalated-by-test" && tr.FromStatus == "new" && tr.ToStatus == "new" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected escalation transition audit row, got %+v", trs)
	}
}

func TestIntegration_IncidentEscalation_OnlyIfUnassigned(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	assignee := "already-assigned"
	item, err := repo.CreateIncident(context.Background(), &domain.Incident{
		Title:    "escalation-unassigned-guard",
		Severity: "warning",
		Source:   "manual",
		Assignee: &assignee,
	})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if _, err := db.Exec(`UPDATE incidents SET created_at = NOW() - interval '40 minutes' WHERE id = $1`, item.ID); err != nil {
		t.Fatalf("force old created_at: %v", err)
	}
	changed, err := repo.EscalateUnackedIncidentsWithFilter(
		context.Background(),
		10*time.Minute,
		"noc-stage1",
		"itest",
		"stage1",
		"",
		"",
		true,
	)
	if err != nil {
		t.Fatalf("EscalateUnackedIncidentsWithFilter: %v", err)
	}
	if changed != 0 {
		t.Fatalf("expected 0 changes for assigned incident, got %d", changed)
	}
	updated, err := repo.GetIncidentByID(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("GetIncidentByID: %v", err)
	}
	if updated == nil || updated.Assignee == nil || *updated.Assignee != assignee {
		t.Fatalf("assignee changed unexpectedly: %+v", updated)
	}
}

func TestIntegration_IncidentDedupAndAutoResolve(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	d := &domain.Device{IP: ip, Name: "incident-dedup", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	details, _ := json.Marshal(map[string]any{"status": "failed_timeout"})
	first, created, err := repo.CreateOrTouchOpenIncident(context.Background(), &d.ID, "SNMP device unavailable", "critical", "polling", details, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident first: %v", err)
	}
	if !created || first == nil {
		t.Fatalf("expected first incident created, got created=%v first=%+v", created, first)
	}
	second, created2, err := repo.CreateOrTouchOpenIncident(context.Background(), &d.ID, "SNMP device unavailable", "critical", "polling", details, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident second: %v", err)
	}
	if created2 {
		t.Fatal("expected second incident to be deduplicated/touched, not created")
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("expected same incident id on dedup, first=%+v second=%+v", first, second)
	}

	resolvedN, err := repo.ResolveOpenIncidentsBySource(context.Background(), d.ID, "polling", "itest", "auto")
	if err != nil {
		t.Fatalf("ResolveOpenIncidentsBySource: %v", err)
	}
	if resolvedN < 1 {
		t.Fatalf("expected at least one resolved incident, got %d", resolvedN)
	}
	item, err := repo.GetIncidentByID(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetIncidentByID: %v", err)
	}
	if item == nil || item.Status != "resolved" {
		t.Fatalf("expected incident resolved, got %+v", item)
	}
}

func TestIntegration_CreateOrTouchOpenIncident_ConcurrentDedup(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	d := &domain.Device{IP: ip, Name: "incident-race", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), d.ID) })

	details, _ := json.Marshal(map[string]any{"status": "failed_timeout"})
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := repo.CreateOrTouchOpenIncident(context.Background(), &d.ID, "SNMP device unavailable", "critical", "polling", details, 10*time.Minute)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("CreateOrTouchOpenIncident concurrent: %v", err)
		}
	}

	var cnt int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM incidents WHERE device_id = $1 AND source = 'polling' AND title = 'SNMP device unavailable' AND status IN ('new','acknowledged','in_progress')`, d.ID,
	).Scan(&cnt); err != nil {
		t.Fatalf("count concurrent polling incidents: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 open polling incident after concurrent dedup, got %d", cnt)
	}
}
