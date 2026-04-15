package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"NMS1/internal/testdb"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func trapIntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		t.Skip("integration: set DB_DSN to run against PostgreSQL")
	}
	return dsn
}

func uniqueTrapIP(t *testing.T) string {
	t.Helper()
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("172.19.%d.%d", b[0], b[1])
}

func openTrapsTestDB(t *testing.T) (*sql.DB, *TrapsRepo) {
	t.Helper()
	dsn := trapIntegrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	testdb.PingDBOrSkip(t, db, 5*time.Second)
	return db, NewTrapsRepo(db)
}

func TestIntegration_TrapsInsertAndList(t *testing.T) {
	_, repo := openTrapsTestDB(t)

	ip := uniqueTrapIP(t)
	oid := "1.3.6.1.6.3.1.1.5.3"
	ctx := context.Background()

	if err := repo.Insert(ctx, ip, oid, 12345, map[string]string{"k": "v"}, false); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	list, err := repo.ByDevice(ctx, ip, 10)
	if err != nil {
		t.Fatalf("ByDevice: %v", err)
	}
	if len(list) != 1 || list[0].OID != oid || list[0].DeviceIP != ip {
		t.Fatalf("ByDevice: %+v", list)
	}

	all, err := repo.List(ctx, 500)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, tr := range all {
		if tr.DeviceIP == ip && tr.OID == oid {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("inserted trap not found in List")
	}
}

func TestIntegration_TrapsInsertEmptyOIDBecomesUnknown(t *testing.T) {
	_, repo := openTrapsTestDB(t)

	ip := uniqueTrapIP(t)
	ctx := context.Background()
	if err := repo.Insert(ctx, ip, "", 0, nil, false); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	list, err := repo.ByDevice(ctx, ip, 5)
	if err != nil || len(list) != 1 || list[0].OID != "unknown" {
		t.Fatalf("want oid unknown: %+v err=%v", list, err)
	}
}

func TestIntegration_TrapsByDeviceRespectsLimit(t *testing.T) {
	_, repo := openTrapsTestDB(t)

	ip := uniqueTrapIP(t)
	ctx := context.Background()

	if err := repo.Insert(ctx, ip, "1.3.6.1.1.1.1", 1, map[string]string{"a": "1"}, false); err != nil {
		t.Fatalf("Insert 1: %v", err)
	}
	if err := repo.Insert(ctx, ip, "1.3.6.1.1.1.2", 2, map[string]string{"b": "2"}, false); err != nil {
		t.Fatalf("Insert 2: %v", err)
	}

	list, err := repo.ByDevice(ctx, ip, 1)
	if err != nil {
		t.Fatalf("ByDevice: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ByDevice limit=1: want 1 row, got %d %+v", len(list), list)
	}
}

func TestIntegration_TrapsByDeviceEmptyWhenNoRows(t *testing.T) {
	_, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	list, err := repo.ByDevice(ctx, ip, 10)
	if err != nil {
		t.Fatalf("ByDevice: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
}

func TestIntegration_TrapsInsertCriticalStored(t *testing.T) {
	db, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	oid := "1.3.6.1.6.3.1.1.5.1"
	if err := repo.Insert(ctx, ip, oid, 0, map[string]string{"x": "y"}, true); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM traps WHERE device_ip = $1::inet`, ip)
	})
	var critical bool
	err := db.QueryRowContext(ctx, `SELECT is_critical FROM traps WHERE device_ip = $1::inet AND oid = $2 ORDER BY id DESC LIMIT 1`, ip, oid).Scan(&critical)
	if err != nil {
		t.Fatalf("query is_critical: %v", err)
	}
	if !critical {
		t.Fatal("expected is_critical=true")
	}
}

func TestIntegration_TrapForDeviceIPAfterDeviceInsert(t *testing.T) {
	db, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	_, err := db.ExecContext(ctx,
		`INSERT INTO devices (ip, name, community, snmp_version) VALUES ($1::inet, $2, 'public', 'v2c')`,
		ip, "integration-trap-device",
	)
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM traps WHERE device_ip = $1::inet`, ip)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM devices WHERE ip = $1::inet`, ip)
	})

	oid := "1.3.6.1.4.1.1.2.3"
	if err := repo.Insert(ctx, ip, oid, 42, map[string]string{"if": "down"}, false); err != nil {
		t.Fatalf("Insert trap: %v", err)
	}
	list, err := repo.ByDevice(ctx, ip, 5)
	if err != nil {
		t.Fatalf("ByDevice: %v", err)
	}
	if len(list) != 1 || list[0].OID != oid {
		t.Fatalf("ByDevice: %+v", list)
	}
}

func TestIntegration_TrapsByDeviceOrdersNewestFirst(t *testing.T) {
	db, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	oidOld := "1.3.6.1.2.1.1.3.0"
	oidNew := "1.3.6.1.2.1.1.5.0"

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM traps WHERE device_ip = $1::inet`, ip)
	})

	if err := repo.Insert(ctx, ip, oidOld, 1, nil, false); err != nil {
		t.Fatalf("Insert old: %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if err := repo.Insert(ctx, ip, oidNew, 2, nil, false); err != nil {
		t.Fatalf("Insert new: %v", err)
	}

	list, err := repo.ByDevice(ctx, ip, 2)
	if err != nil {
		t.Fatalf("ByDevice: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 rows, got %d %+v", len(list), list)
	}
	if list[0].OID != oidNew || list[1].OID != oidOld {
		t.Fatalf("expected newest OID first: got %+v", list)
	}
}

func TestIntegration_TrapCorrelation_LinkLossAndRecovery(t *testing.T) {
	db, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	_, err := db.ExecContext(ctx,
		`INSERT INTO devices (ip, name, community, snmp_version) VALUES ($1::inet, $2, 'public', 'v2c')`,
		ip, "integration-trap-correlation",
	)
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM incident_transitions WHERE incident_id IN (SELECT id FROM incidents WHERE source='trap')`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM incidents WHERE source='trap'`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM traps WHERE device_ip = $1::inet`, ip)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM devices WHERE ip = $1::inet`, ip)
	})

	if err := repo.CreateOrTouchOpenTrapIncident(ctx, ip, "IF-MIB::linkDown", map[string]string{"ifName": "Gi0/1"}, 10*time.Minute); err != nil {
		t.Fatalf("CreateOrTouch linkDown: %v", err)
	}
	if err := repo.CreateOrTouchOpenTrapIncident(ctx, ip, "BFD-MIB::bfdDown", map[string]string{"peer": "10.0.0.1"}, 10*time.Minute); err != nil {
		t.Fatalf("CreateOrTouch bfdDown: %v", err)
	}

	var cnt int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM incidents WHERE source='trap' AND title='Link loss detected' AND status IN ('new','acknowledged','in_progress')`).Scan(&cnt)
	if err != nil {
		t.Fatalf("count incidents: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 open link loss incident after correlation, got %d", cnt)
	}

	if err := repo.CreateOrTouchOpenTrapIncident(ctx, ip, "IF-MIB::linkUp", map[string]string{"ifName": "Gi0/1"}, 10*time.Minute); err != nil {
		t.Fatalf("CreateOrTouch linkUp: %v", err)
	}

	var status string
	err = db.QueryRowContext(ctx,
		`SELECT status FROM incidents WHERE source='trap' AND title='Link loss detected' ORDER BY id DESC LIMIT 1`).Scan(&status)
	if err != nil {
		t.Fatalf("get incident status: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("expected resolved status after recovery trap, got %q", status)
	}
}

func TestIntegration_TrapCorrelation_UsesOIDMappingTable(t *testing.T) {
	db, repo := openTrapsTestDB(t)
	ctx := context.Background()
	ip := uniqueTrapIP(t)
	_, err := db.ExecContext(ctx,
		`INSERT INTO devices (ip, name, community, snmp_version) VALUES ($1::inet, $2, 'public', 'v2c')`,
		ip, "integration-trap-mapping",
	)
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO trap_oid_mappings (vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled)
		VALUES ('test_vendor', '%acmealarm%', 'generic', 'ACME alarm detected', 'critical', FALSE, 9999, TRUE)
		ON CONFLICT (vendor, oid_pattern, signal_kind) DO UPDATE
		   SET title = EXCLUDED.title,
		       severity = EXCLUDED.severity,
		       is_recovery = EXCLUDED.is_recovery,
		       priority = EXCLUDED.priority,
		       enabled = EXCLUDED.enabled`)
	if err != nil {
		t.Fatalf("insert mapping: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM incident_transitions WHERE incident_id IN (SELECT id FROM incidents WHERE source='trap')`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM incidents WHERE source='trap'`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM trap_oid_mappings WHERE vendor='test_vendor'`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM devices WHERE ip = $1::inet`, ip)
	})

	if err := repo.CreateOrTouchOpenTrapIncident(ctx, ip, "ACME-MIB::acmeAlarm", map[string]string{"slot": "1"}, 10*time.Minute); err != nil {
		t.Fatalf("CreateOrTouch custom mapping: %v", err)
	}

	var title, severity string
	err = db.QueryRowContext(ctx,
		`SELECT title, severity FROM incidents WHERE source='trap' AND device_id = (SELECT id FROM devices WHERE ip = $1::inet) ORDER BY id DESC LIMIT 1`, ip).
		Scan(&title, &severity)
	if err != nil {
		t.Fatalf("query incident by custom mapping: %v", err)
	}
	if title != "ACME alarm detected" {
		t.Fatalf("expected mapped title, got %q", title)
	}
	if severity != "critical" {
		t.Fatalf("expected mapped severity critical, got %q", severity)
	}
}
