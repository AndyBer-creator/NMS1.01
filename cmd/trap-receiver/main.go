package main

import (
	"NMS1/internal/repository"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
	_ "github.com/jackc/pgx/v5/stdlib"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	// ✅ ТОТ ЖЕ logger что в worker!
	logger := setupLogger("nms-trap-receiver")
	defer logger.Sync()

	logger.Info("🚀 SNMP Trap Receiver v1 started")

	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		logger.Fatal("DB_DSN is required for trap persistence")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Fatal("DB open failed", zap.Error(err))
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		logger.Fatal("DB ping failed", zap.Error(err))
	}
	repo := repository.NewTrapsRepo(db)

	port := uint16(162)
	if p := strings.TrimSpace(os.Getenv("TRAP_PORT")); p != "" {
		if parsed, perr := strconv.ParseUint(p, 10, 16); perr == nil {
			port = uint16(parsed)
		}
	}

	tl := gosnmp.NewTrapListener()
	tl.Params = gosnmp.Default
	tl.Params.Port = port
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
		if err := repo.Insert(context.Background(), addr.IP.String(), trapOID, uptime, vars, false); err != nil {
			logger.Error("Failed to persist trap",
				zap.String("from", addr.IP.String()),
				zap.String("oid", trapOID),
				zap.Error(err))
			return
		}

		raw, _ := json.Marshal(vars)
		logger.Info("SNMP trap persisted",
			zap.String("from", addr.IP.String()),
			zap.String("oid", trapOID),
			zap.Int64("uptime", uptime),
			zap.ByteString("vars", raw))
	}

	logger.Info("Listening SNMP traps", zap.String("addr", fmt.Sprintf("0.0.0.0:%d/udp", port)))
	if err := tl.Listen(fmt.Sprintf("0.0.0.0:%d", port)); err != nil {
		logger.Fatal("Trap listener failed", zap.Error(err))
	}
}

// ✅ ТОТ ЖЕ setupLogger из worker!
func setupLogger(serviceName string) *zap.Logger {
	logDir := "./logs"
	if os.Getenv("NMS_ENV") == "docker" {
		logDir = "/app/logs"
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create log dir %s: %v", logDir, err))
	}

	hook := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, serviceName+".log"),
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
		LocalTime:  true,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(hook),
		zapcore.InfoLevel,
	)

	return zap.New(core, zap.AddCaller())
}
