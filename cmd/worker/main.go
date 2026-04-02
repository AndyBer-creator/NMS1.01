package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/usecases/lldp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	cfg := config.Load()

	// ПРОДАКШЕН logger с ротацией
	logger := setupLogger("nms-worker")
	defer logger.Sync()

	repo, err := postgres.New(cfg.DB.DSN)
	if err != nil {
		logger.Fatal("DB failed", zap.Error(err))
	}
	defer repo.Close()

	snmpClient := snmp.New(
		int(cfg.SNMP.Port),
		time.Duration(cfg.SNMP.Timeout)*time.Second,
		cfg.SNMP.Retries,
	)

	ticker := time.NewTicker(1 * time.Minute)
	lldpTicker := time.NewTicker(5 * time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer ticker.Stop()
	defer lldpTicker.Stop()

	logger.Info("🚀 NMS Worker v3 started (prod logging)")

	for {
		select {
		case <-ctx.Done():
			logger.Info("🛑 Worker shutdown")
			return
		case <-ticker.C:
			logger.Info("=== Polling cycle ===", zap.Int("interval_sec", 60))

			start := time.Now()
			if err := pollAllDevices(ctx, repo, snmpClient, logger); err != nil {
				logger.Error("Polling failed", zap.Error(err))
			}

			logger.Info("=== Cycle complete ===",
				zap.Duration("duration", time.Since(start)))
		case <-lldpTicker.C:
			logger.Info("=== LLDP topology cycle ===", zap.String("interval", "5m"))
			start := time.Now()
			if _, err := lldp.ScanAllDevicesLLDP(ctx, repo, snmpClient, logger, lldp.ScanParams{}); err != nil {
				logger.Error("LLDP scan failed", zap.Error(err))
			}
			logger.Info("=== LLDP cycle complete ===", zap.Duration("duration", time.Since(start)))
		}
	}
}

func setupLogger(serviceName string) *zap.Logger {
	// ✅ Dev: ./logs/  Docker: /app/logs/
	logDir := "./logs"
	if os.Getenv("NMS_ENV") == "docker" {
		logDir = "/app/logs"
	}

	// Создай папку если нет
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

	logger := zap.New(core, zap.AddCaller())
	return logger
}

func pollAllDevices(ctx context.Context, repo *postgres.Repo, snmpClient *snmp.Client, logger *zap.Logger) error {
	select {
	case <-ctx.Done():
		logger.Info("Polling cancelled")
		return ctx.Err()
	default:
	}

	devices, err := repo.ListDevices()
	if err != nil {
		logger.Error("Failed to list devices", zap.Error(err))
		return err
	}

	baseOids := config.StandardOIDs()
	logger.Info("Starting poll",
		zap.Int("devices", len(devices)),
		zap.Int("oids", len(baseOids)))

	failed, success := 0, 0

	for _, device := range devices {
		select {
		case <-ctx.Done():
			logger.Info("Polling interrupted",
				zap.Int("processed", success+failed))
			return ctx.Err()
		default:
		}

		// ✅ ИСПРАВЛЕНО: правильный синтаксис zap
		logger.Info("Polling device",
			zap.Int("id", device.ID),
			zap.String("ip", device.IP),
			zap.String("name", device.Name),
			zap.String("version", device.SNMPVersion))

		result, err := snmpClient.GetDevice(device, baseOids)
		if err != nil {
			// ✅ ИСПРАВЛЕНО: НЕ append, а отдельные поля
			logger.Warn("SNMP failed",
				zap.Int("id", device.ID),
				zap.String("ip", device.IP),
				zap.String("version", device.SNMPVersion),
				zap.Error(err))

			repo.UpdateDeviceStatus(device.ID, "failed")
			failed++
			continue
		}

		metricsSaved := 0
		for oid, value := range result {
			if err := repo.SaveMetric(device.ID, oid, value); err != nil {
				logger.Warn("Save metric failed",
					zap.String("ip", device.IP),
					zap.String("oid", oid),
					zap.Error(err))
			} else {
				metricsSaved++
			}
		}

		repo.UpdateDeviceLastSeen(device.ID)
		repo.UpdateDeviceStatus(device.ID, "active")
		success++

		sysDescr := getValue(result, "1.3.6.1.2.1.1.1.0")
		logger.Info("✅ Device polled OK",
			zap.String("ip", device.IP),
			zap.String("sysDescr", sysDescr),
			zap.Int("metrics", len(result)),
			zap.Int("saved", metricsSaved))
	}

	logger.Info("Polling stats",
		zap.Int("success", success),
		zap.Int("failed", failed),
		zap.Int("total", len(devices)))

	return nil
}

func getValue(result map[string]string, oid string) string {
	if val, ok := result[oid]; ok {
		return val
	}
	return "N/A"
}
