package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLldpRepoMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("create scan and latest id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectQuery(`INSERT INTO lldp_topology_scans DEFAULT VALUES RETURNING id`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
		id, err := repo.CreateLldpScan(ctx)
		if err != nil || id != 11 {
			t.Fatalf("CreateLldpScan: id=%d err=%v", id, err)
		}

		mock.ExpectQuery(`SELECT id FROM lldp_topology_scans ORDER BY id DESC LIMIT 1`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
		latest, err := repo.GetLatestLldpScanID(ctx)
		if err != nil || latest != 11 {
			t.Fatalf("GetLatestLldpScanID: id=%d err=%v", latest, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("delete scan handles invalid and success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		if err := repo.DeleteLldpScan(ctx, 0); err != nil {
			t.Fatalf("DeleteLldpScan invalid id: %v", err)
		}

		mock.ExpectExec(`DELETE FROM lldp_topology_scans WHERE id = \$1`).
			WithArgs(15).
			WillReturnResult(sqlmock.NewResult(0, 1))
		if err := repo.DeleteLldpScan(ctx, 15); err != nil {
			t.Fatalf("DeleteLldpScan: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("insert link returns affected rows", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		remoteIP := "10.0.0.2"
		mock.ExpectExec(`INSERT INTO lldp_links`).
			WithArgs(int64(3), "10.0.0.1", 7, "Gi0/1", &remoteIP, "sw2", "desc", "Eth1", "uplink").
			WillReturnResult(sqlmock.NewResult(0, 1))
		affected, err := repo.InsertLldpLink(ctx, 3, LldpLink{
			LocalDeviceIP:  "10.0.0.1",
			LocalPortNum:   7,
			LocalPortDesc:  "Gi0/1",
			RemoteDeviceIP: &remoteIP,
			RemoteSysName:  "sw2",
			RemoteSysDesc:  "desc",
			RemotePortID:   "Eth1",
			RemotePortDesc: "uplink",
		})
		if err != nil || affected != 1 {
			t.Fatalf("InsertLldpLink: affected=%d err=%v", affected, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("latest links returns nil when no scans", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectQuery(`SELECT id FROM lldp_topology_scans ORDER BY id DESC LIMIT 1`).
			WillReturnError(sql.ErrNoRows)
		links, err := repo.GetLatestLldpLinks(ctx)
		if err != nil || links != nil {
			t.Fatalf("expected nil,nil, got links=%v err=%v", links, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("latest links returns scanned rows", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectQuery(`SELECT id FROM lldp_topology_scans ORDER BY id DESC LIMIT 1`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
		mock.ExpectQuery(`FROM lldp_links l`).
			WithArgs(int64(21)).
			WillReturnRows(sqlmock.NewRows([]string{
				"local_ip", "local_name", "local_port_num", "local_port_desc", "remote_ip",
				"remote_name", "remote_sys_name", "remote_sys_desc", "remote_port_id", "remote_port_desc",
			}).AddRow(
				"10.0.0.1", "sw1", 1, "Gi0/1", "10.0.0.2", "sw2", "remote-sw2", "desc", "Eth1", "uplink",
			))

		links, err := repo.GetLatestLldpLinks(ctx)
		if err != nil {
			t.Fatalf("GetLatestLldpLinks: %v", err)
		}
		if len(links) != 1 || links[0].RemoteIP == nil || *links[0].RemoteIP != "10.0.0.2" {
			t.Fatalf("unexpected links: %+v", links)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})
}
