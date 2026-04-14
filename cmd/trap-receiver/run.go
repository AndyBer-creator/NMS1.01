package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"NMS1/internal/repository"

	"github.com/gosnmp/gosnmp"
	_ "github.com/jackc/pgx/v5/stdlib"

	"go.uber.org/zap"
)

// run слушает SNMP traps на udpPort до отмены ctx.
func run(ctx context.Context, log *zap.Logger, dsn string, udpPort uint16) error {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return fmt.Errorf("DB_DSN is required for trap persistence")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("db open: %w", err)
	}
	defer func() { _ = db.Close() }()

	pingCtx, pingCancel := context.WithTimeout(ctx, 15*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}

	repo := repository.NewTrapsRepo(db)

	tl := gosnmp.NewTrapListener()
	tl.Params = gosnmp.Default
	tl.Params.Port = udpPort
	tl.OnNewTrap = func(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
		trapOID := "unknown"
		vars := make(map[string]string)
		for _, v := range packet.Variables {
			val := fmt.Sprintf("%v", v.Value)
			vars[v.Name] = val
			if v.Name == ".1.3.6.1.6.3.1.1.4.1.0" || v.Name == "1.3.6.1.6.3.1.1.4.1.0" {
				if val != "" {
					trapOID = val
				}
			}
		}
		if trapOID == "unknown" && packet.Enterprise != "" {
			trapOID = packet.Enterprise
		}

		uptime := int64(packet.Timestamp)
		insertCtx, insertCancel := context.WithTimeout(ctx, 5*time.Second)
		err := repo.Insert(insertCtx, addr.IP.String(), trapOID, uptime, vars, false)
		insertCancel()
		if err != nil {
			log.Error("Failed to persist trap",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Error(err))
			return
		}

		raw, _ := json.Marshal(vars)
		log.Info("SNMP trap persisted",
			zap.String("from", addr.IP.String()),
			zap.String("oid", trapOID),
			zap.Int64("uptime", uptime),
			zap.ByteString("vars", raw))
	}

	addr := fmt.Sprintf("0.0.0.0:%d", udpPort)
	log.Info("Listening SNMP traps", zap.String("addr", addr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- tl.Listen(addr)
	}()

	select {
	case <-ctx.Done():
		tl.Close()
		<-errCh
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("trap listen: %w", err)
		}
		return nil
	}
}
