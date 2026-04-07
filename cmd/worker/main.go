package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/usecases/lldp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	workerPollDurationSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nms_worker_poll_duration_seconds",
		Help:    "Duration of worker SNMP polling cycle in seconds",
		Buckets: prometheus.DefBuckets,
	})
	workerPollDevicesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nms_worker_poll_devices_total",
			Help: "Total devices processed by worker SNMP polling",
		},
		[]string{"status"},
	)

	lldpScanDurationSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nms_lldp_scan_duration_seconds",
		Help:    "Duration of LLDP scan in seconds",
		Buckets: prometheus.DefBuckets,
	})
	lldpLinksFoundGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nms_lldp_links_found",
		Help: "Links found by the last LLDP scan (best-effort)",
	})
	lldpLinksInsertedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nms_lldp_links_inserted",
		Help: "Links inserted into DB by the last LLDP scan",
	})
	lldpLinksInsertedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nms_lldp_links_inserted_total",
		Help: "Total links inserted into DB by worker LLDP scans",
	})

	// LLDP может идти минутами; без этого блокировался главный select и пропускались циклы SNMP-опроса (1 мин).
	lldpBusy atomic.Bool
)

func init() {
	prometheus.MustRegister(workerPollDurationSeconds)
	prometheus.MustRegister(workerPollDevicesTotal)
	prometheus.MustRegister(lldpScanDurationSeconds)
	prometheus.MustRegister(lldpLinksFoundGauge)
	prometheus.MustRegister(lldpLinksInsertedGauge)
	prometheus.MustRegister(lldpLinksInsertedTotal)
}

func main() {
	cfg := config.Load()

	// ПРОДАКШЕН logger с ротацией
	logger := setupLogger("nms-worker")
	defer logger.Sync()

	if err := startMetricsServer(":8081", logger); err != nil {
		logger.Warn("Metrics server failed to start", zap.Error(err))
	}

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
			success, failed, err := pollAllDevices(ctx, repo, snmpClient, logger)
			workerPollDurationSeconds.Observe(time.Since(start).Seconds())
			if success > 0 {
				workerPollDevicesTotal.WithLabelValues("active").Add(float64(success))
			}
			if failed > 0 {
				workerPollDevicesTotal.WithLabelValues("failed").Add(float64(failed))
			}
			if err != nil {
				logger.Error("Polling failed", zap.Error(err))
			}

			logger.Info("=== Cycle complete ===",
				zap.Duration("duration", time.Since(start)))
		case <-lldpTicker.C:
			if !lldpBusy.CompareAndSwap(false, true) {
				logger.Warn("LLDP scan skipped: previous run still in progress")
			} else {
				go func() {
					defer lldpBusy.Store(false)
					logger.Info("=== LLDP topology cycle ===", zap.String("interval", "5m"))
					start := time.Now()
					summary, err := lldp.ScanAllDevicesLLDP(ctx, repo, snmpClient, logger, lldp.ScanParams{})
					lldpScanDurationSeconds.Observe(time.Since(start).Seconds())
					if summary != nil {
						lldpLinksFoundGauge.Set(float64(summary.LinksFound))
						lldpLinksInsertedGauge.Set(float64(summary.LinksInserted))
						lldpLinksInsertedTotal.Add(float64(summary.LinksInserted))
					}
					if err != nil {
						logger.Error("LLDP scan failed", zap.Error(err))
					}
					logger.Info("=== LLDP cycle complete ===", zap.Duration("duration", time.Since(start)))
				}()
			}
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

func startMetricsServer(addr string, logger *zap.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Worker metrics server started", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Worker metrics server error", zap.Error(err))
		}
	}()
	return nil
}

func pollAllDevices(ctx context.Context, repo *postgres.Repo, snmpClient *snmp.Client, logger *zap.Logger) (success int, failed int, err error) {
	select {
	case <-ctx.Done():
		logger.Info("Polling cancelled")
		return 0, 0, ctx.Err()
	default:
	}

	devices, err := repo.ListDevices()
	if err != nil {
		logger.Error("Failed to list devices", zap.Error(err))
		return 0, 0, err
	}

	baseOids := config.StandardOIDs()
	logger.Info("Starting poll",
		zap.Int("devices", len(devices)),
		zap.Int("oids", len(baseOids)))

	for _, device := range devices {
		select {
		case <-ctx.Done():
			logger.Info("Polling interrupted",
				zap.Int("processed", success+failed))
			return success, failed, ctx.Err()
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
			status := "failed_transport"
			switch snmp.GetErrorKind(err) {
			case snmp.ErrorKindTimeout:
				status = "failed_timeout"
			case snmp.ErrorKindAuth:
				status = "failed_auth"
			case snmp.ErrorKindNoSuch:
				status = "failed_no_such_name"
			case snmp.ErrorKindTransport:
				status = "failed_transport"
			}
			// ✅ ИСПРАВЛЕНО: НЕ append, а отдельные поля
			logger.Warn("SNMP failed",
				zap.Int("id", device.ID),
				zap.String("ip", device.IP),
				zap.String("version", device.SNMPVersion),
				zap.String("error_kind", status),
				zap.Error(err))

			_ = repo.UpdateDeviceError(device.ID, status, err.Error())
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

		_ = repo.MarkDevicePollSuccess(device.ID)
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

	return success, failed, nil
}

func getValue(result map[string]string, oid string) string {
	if val, ok := result[oid]; ok {
		return val
	}
	return "N/A"
}
