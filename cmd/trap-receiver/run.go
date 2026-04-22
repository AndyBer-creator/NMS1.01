package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/grpcapi"
	"NMS1/internal/repository"

	"github.com/gosnmp/gosnmp"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.uber.org/zap"
)

// run слушает SNMP traps на udpPort до отмены ctx.
func run(ctx context.Context, log *zap.Logger, dsn string, grpcTarget string, udpPort uint16) error {
	dsn = strings.TrimSpace(dsn)
	grpcTarget = strings.TrimSpace(grpcTarget)
	useGRPC := grpcTarget != ""
	if !useGRPC && dsn == "" {
		return fmt.Errorf("DB_DSN is required when NMS_TRAP_GRPC_TARGET is not set")
	}

	var (
		repo   *repository.TrapsRepo
		db     *sql.DB
		client grpcapi.TrapServiceClient
		conn   *grpc.ClientConn
		err    error
	)

	if useGRPC {
		conn, err = grpc.NewClient(
			grpcTarget,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcapi.JSONCodec{})),
		)
		if err != nil {
			return fmt.Errorf("grpc dial: %w", err)
		}
		defer func() { _ = conn.Close() }()
		client = grpcapi.NewTrapServiceClient(conn)
		log.Info("trap forwarding via gRPC enabled", zap.String("target", grpcTarget))
	} else {
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("db open: %w", err)
		}
		defer func() { _ = db.Close() }()

		pingCtx, pingCancel := context.WithTimeout(ctx, 15*time.Second)
		defer pingCancel()
		if err := db.PingContext(pingCtx); err != nil {
			return fmt.Errorf("db ping: %w", err)
		}
		repo = repository.NewTrapsRepo(db)
	}
	suppressionWindow := trapIncidentSuppressionWindow()

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
		if useGRPC {
			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := client.IngestTrap(callCtx, &grpcapi.TrapIngestRequest{
				DeviceIP: addr.IP.String(),
				OID:      trapOID,
				Uptime:   uptime,
				TrapVars: vars,
			})
			callCancel()
			if err != nil {
				log.Error("Failed to forward trap via gRPC",
					zap.String("from", addr.IP.String()),
					zap.String("oid", trapOID),
					zap.Error(err))
				return
			}
		} else {
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
			incidentCtx, incidentCancel := context.WithTimeout(ctx, 5*time.Second)
			if err := repo.CreateOrTouchOpenTrapIncident(incidentCtx, addr.IP.String(), trapOID, vars, suppressionWindow); err != nil {
				log.Warn("Failed to correlate trap into incident",
					zap.String("from", addr.IP.String()),
					zap.String("oid", trapOID),
					zap.Error(err))
			}
			incidentCancel()
		}

		if useGRPC {
			log.Info("SNMP trap forwarded",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Int64("uptime", uptime))
		} else {
			log.Info("SNMP trap persisted",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Int64("uptime", uptime))
		}
		raw, _ := json.Marshal(vars)
		log.Debug("SNMP trap vars", zap.ByteString("vars", raw))
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

func trapIncidentSuppressionWindow() time.Duration {
	raw := strings.TrimSpace(config.EnvOrFile("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW"))
	if raw == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}
