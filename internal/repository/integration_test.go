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

func TestIntegration_TrapsInsertAndList(t *testing.T) {
	dsn := trapIntegrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	testdb.PingDBOrSkip(t, db, 5*time.Second)

	ip := uniqueTrapIP(t)
	oid := "1.3.6.1.6.3.1.1.5.3"
	repo := NewTrapsRepo(db)
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
	dsn := trapIntegrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	testdb.PingDBOrSkip(t, db, 5*time.Second)

	ip := uniqueTrapIP(t)
	repo := NewTrapsRepo(db)
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
	dsn := trapIntegrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	testdb.PingDBOrSkip(t, db, 5*time.Second)

	ip := uniqueTrapIP(t)
	repo := NewTrapsRepo(db)
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
