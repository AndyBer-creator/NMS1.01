// Интеграционные тесты слоя postgres.Repo для устройств (devices).
// Требуется DB_DSN и доступный PostgreSQL; иначе t.Skip через testdb.PingDBOrSkip.
// См. также integration_test.go (настройки, LLDP, события доступности и т.д.).

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"NMS1/internal/domain"
)

func TestIntegration_DeviceCreateGetListDelete(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(context.Background(), ip)

	d := &domain.Device{
		IP:          ip,
		Name:        "integration-device",
		Community:   "public",
		SNMPVersion: "v2c",
	}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if d.ID <= 0 {
		t.Fatal("expected positive device id")
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	got, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil {
		t.Fatalf("GetDeviceByIP: %v", err)
	}
	if got == nil || got.Name != d.Name || got.Community != "public" {
		t.Fatalf("GetDeviceByIP: %+v", got)
	}

	list, err := repo.ListDevices(context.Background())
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

	if err := repo.DeleteByIP(context.Background(), ip); err != nil {
		t.Fatalf("DeleteByIP: %v", err)
	}
	after, err := repo.GetDeviceByIP(context.Background(), ip)
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
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	d := &domain.Device{IP: ip, Name: "x", Community: "c", SNMPVersion: "v9"}
	if err := repo.CreateDevice(context.Background(), d); err == nil {
		t.Fatal("expected error for invalid snmp_version")
	}
}

func TestIntegration_UpdateDeviceByIPAndMetrics(t *testing.T) {
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(context.Background(), ip)

	d := &domain.Device{IP: ip, Name: "before", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	updated, err := repo.UpdateDeviceByIP(context.Background(), ip, &domain.Device{Name: "after", Community: "private", SNMPVersion: "v3", AuthProto: "MD5", AuthPass: "a", PrivProto: "DES", PrivPass: "p"})
	if err != nil {
		t.Fatalf("UpdateDeviceByIP: %v", err)
	}
	if updated.Name != "after" || updated.SNMPVersion != "v3" {
		t.Fatalf("patch: %+v", updated)
	}

	oid := "1.3.6.1.2.1.1.5.0"
	val := "itest-hostname"
	if err := repo.SaveMetric(context.Background(), d.ID, oid, val); err != nil {
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
	_ = repo.DeleteByIP(context.Background(), ip)

	d := &domain.Device{IP: ip, Name: "health", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	if err := repo.UpdateDeviceError(d.ID, "failed", "timeout"); err != nil {
		t.Fatalf("UpdateDeviceError: %v", err)
	}
	afterErr, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || afterErr == nil || afterErr.Status != "failed" || !strings.Contains(afterErr.LastError, "timeout") {
		t.Fatalf("after error: %+v err=%v", afterErr, err)
	}

	if err := repo.MarkDevicePollSuccess(d.ID); err != nil {
		t.Fatalf("MarkDevicePollSuccess: %v", err)
	}
	ok, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || ok == nil || ok.Status != "active" || ok.LastError != "" {
		t.Fatalf("after success: %+v", ok)
	}
	if ok.LastPollOKAt.IsZero() {
		t.Fatal("expected last_poll_ok_at set")
	}

	if err := repo.InsertSNMPSetAudit(context.Background(), SNMPSetAuditRecord{UserName: "admin", DeviceID: sql.NullInt64{Int64: int64(d.ID), Valid: true}, OID: "1.2.3", OldValue: "a", NewValue: "b", Result: "ok"}); err != nil {
		t.Fatalf("InsertSNMPSetAudit: %v", err)
	}
	if err := repo.InsertSNMPSetAudit(context.Background(), SNMPSetAuditRecord{OID: "", Result: "x"}); err == nil {
		t.Fatal("empty OID must fail")
	}
}

func TestIntegration_DeleteByIP_NotFound(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	// TEST-NET-1 (RFC 5737): не пересекается с uniqueInet 172.19.0.0/16 в этих тестах.
	if err := repo.DeleteByIP(context.Background(), "192.0.2.199"); err != sql.ErrNoRows {
		t.Fatalf("want sql.ErrNoRows, got %v", err)
	}
}

func TestIntegration_DeviceRepo_GetDeviceByIP_NotFoundReturnsNil(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	got, err := repo.GetDeviceByIP(context.Background(), "192.0.2.210")
	if err != nil {
		t.Fatalf("GetDeviceByIP: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown IP, got %+v", got)
	}
}

func TestIntegration_DeviceRepo_UpdateDeviceLastSeen(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(context.Background(), ip)
	d := &domain.Device{IP: ip, Name: "lastseen", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	before, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || before == nil {
		t.Fatalf("GetDeviceByIP: %v %+v", err, before)
	}
	time.Sleep(50 * time.Millisecond)
	if err := repo.UpdateDeviceLastSeen(d.ID); err != nil {
		t.Fatalf("UpdateDeviceLastSeen: %v", err)
	}
	after, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || after == nil {
		t.Fatalf("GetDeviceByIP: %v", err)
	}
	if !after.LastSeen.After(before.LastSeen) {
		t.Fatalf("expected last_seen to advance: before=%v after=%v", before.LastSeen, after.LastSeen)
	}
}

func TestIntegration_DeviceRepo_UpdateDeviceStatus(t *testing.T) {
	repo, _ := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(context.Background(), ip)
	d := &domain.Device{IP: ip, Name: "status", Community: "public", SNMPVersion: "v2c"}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	if err := repo.UpdateDeviceStatus(d.ID, "maintenance"); err != nil {
		t.Fatalf("UpdateDeviceStatus: %v", err)
	}
	got, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || got == nil || got.Status != "maintenance" {
		t.Fatalf("status: %+v err=%v", got, err)
	}
}

func TestIntegration_DeviceRepo_CreateDevice_SNMPv3RoundTrip(t *testing.T) {
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "itest-db-enc-key")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	_ = repo.DeleteByIP(context.Background(), ip)
	d := &domain.Device{
		IP:          ip,
		Name:        "snmpv3",
		Community:   "snmpuser",
		SNMPVersion: "v3",
		AuthProto:   "SHA",
		AuthPass:    "auth-secret",
		PrivProto:   "AES",
		PrivPass:    "priv-secret",
	}
	if err := repo.CreateDevice(context.Background(), d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	got, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || got == nil {
		t.Fatalf("GetDeviceByIP: %v %+v", err, got)
	}
	if got.SNMPVersion != "v3" || got.AuthProto != "SHA" || got.AuthPass != "auth-secret" {
		t.Fatalf("v3 auth fields: %+v", got)
	}
	if got.PrivProto != "AES" || got.PrivPass != "priv-secret" {
		t.Fatalf("v3 priv fields: %+v", got)
	}
	var communityPlain, authPlain, privPlain sql.NullString
	var communityEnc, authEnc, privEnc sql.NullString
	err = db.QueryRowContext(context.Background(),
		`SELECT community, auth_pass, priv_pass, community_enc, auth_pass_enc, priv_pass_enc
         FROM devices WHERE ip = $1::inet`,
		ip,
	).Scan(&communityPlain, &authPlain, &privPlain, &communityEnc, &authEnc, &privEnc)
	if err != nil {
		t.Fatalf("raw secrets query: %v", err)
	}
	if communityPlain.Valid || authPlain.Valid || privPlain.Valid {
		t.Fatalf("expected plaintext secret columns to be NULL, got community=%+v auth=%+v priv=%+v", communityPlain, authPlain, privPlain)
	}
	if !communityEnc.Valid || !authEnc.Valid || !privEnc.Valid {
		t.Fatalf("expected encrypted secret columns to be set, got community=%+v auth=%+v priv=%+v", communityEnc, authEnc, privEnc)
	}
}

func TestIntegration_DeviceRepo_LegacyPlainSecretsAutoMigratedOnRead(t *testing.T) {
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "itest-db-enc-key")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	repo, db := openIntegrationRepo(t)
	ip := uniqueInet(t)
	t.Cleanup(func() { _ = repo.DeleteByIP(context.Background(), ip) })

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO devices (ip, name, community, snmp_version, auth_proto, auth_pass, priv_proto, priv_pass, status)
		VALUES ($1::inet, $2, $3, 'v3', 'SHA', $4, 'AES', $5, 'active')`,
		ip, "legacy-plain", "public", "auth-legacy", "priv-legacy",
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	got, err := repo.GetDeviceByIP(context.Background(), ip)
	if err != nil || got == nil {
		t.Fatalf("GetDeviceByIP: %v %+v", err, got)
	}
	if got.Community != "public" || got.AuthPass != "auth-legacy" || got.PrivPass != "priv-legacy" {
		t.Fatalf("unexpected decoded values: %+v", got)
	}

	var communityPlain, authPlain, privPlain sql.NullString
	var communityEnc, authEnc, privEnc sql.NullString
	err = db.QueryRowContext(context.Background(),
		`SELECT community, auth_pass, priv_pass, community_enc, auth_pass_enc, priv_pass_enc
         FROM devices WHERE ip = $1::inet`,
		ip,
	).Scan(&communityPlain, &authPlain, &privPlain, &communityEnc, &authEnc, &privEnc)
	if err != nil {
		t.Fatalf("raw secrets query: %v", err)
	}
	if communityPlain.Valid || authPlain.Valid || privPlain.Valid {
		t.Fatalf("expected legacy plaintext to be cleared, got community=%+v auth=%+v priv=%+v", communityPlain, authPlain, privPlain)
	}
	if !communityEnc.Valid || !authEnc.Valid || !privEnc.Valid {
		t.Fatalf("expected encrypted columns after lazy migration, got community=%+v auth=%+v priv=%+v", communityEnc, authEnc, privEnc)
	}
}
