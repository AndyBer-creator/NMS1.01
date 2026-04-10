package main

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
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

	lldpBusy atomic.Bool

	pollBackoff = newDeviceBackoff()
)

type backoffState struct {
	failures int
	nextTry  time.Time
	lastErr  string
}

type deviceBackoff struct {
	mu     sync.Mutex
	byIP   map[string]backoffState
	maxGap time.Duration
}

func newDeviceBackoff() *deviceBackoff {
	return &deviceBackoff{
		byIP:   make(map[string]backoffState),
		maxGap: 15 * time.Minute,
	}
}

func (b *deviceBackoff) shouldSkip(ip string, now time.Time) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.byIP[ip]
	if !ok || st.nextTry.IsZero() || !now.Before(st.nextTry) {
		return false, 0
	}
	return true, st.nextTry.Sub(now)
}

func (b *deviceBackoff) onFailure(ip, errText string, now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.byIP[ip]
	st.failures++
	if st.failures > 5 {
		st.failures = 5
	}
	delay := time.Minute * time.Duration(1<<uint(st.failures-1))
	if delay > b.maxGap {
		delay = b.maxGap
	}
	st.nextTry = now.Add(delay)
	st.lastErr = errText
	b.byIP[ip] = st
	return delay
}

func (b *deviceBackoff) onSuccess(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byIP, ip)
}

func init() {
	prometheus.MustRegister(workerPollDurationSeconds)
	prometheus.MustRegister(workerPollDevicesTotal)
	prometheus.MustRegister(lldpScanDurationSeconds)
	prometheus.MustRegister(lldpLinksFoundGauge)
	prometheus.MustRegister(lldpLinksInsertedGauge)
	prometheus.MustRegister(lldpLinksInsertedTotal)
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

	skipped := 0
	for _, device := range devices {
		select {
		case <-ctx.Done():
			logger.Info("Polling interrupted",
				zap.Int("processed", success+failed))
			return success, failed, ctx.Err()
		default:
		}

		now := time.Now()
		if skip, wait := pollBackoff.shouldSkip(device.IP, now); skip {
			skipped++
			logger.Info("Polling skipped by backoff",
				zap.Int("id", device.ID),
				zap.String("ip", device.IP),
				zap.Duration("retry_in", wait))
			continue
		}

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
			if snmpPollWasOK(device.Status) {
				detail := status + ": " + err.Error()
				if errEv := repo.InsertAvailabilityEvent(device.ID, "unavailable", detail); errEv != nil {
					logger.Warn("availability event insert failed", zap.Int("device_id", device.ID), zap.Error(errEv))
				}
			}
			logger.Warn("SNMP failed",
				zap.Int("id", device.ID),
				zap.String("ip", device.IP),
				zap.String("version", device.SNMPVersion),
				zap.String("error_kind", status),
				zap.Error(err))

			retryAfter := pollBackoff.onFailure(device.IP, err.Error(), time.Now())
			_ = repo.UpdateDeviceError(device.ID, status, err.Error())
			logger.Warn("Backoff scheduled",
				zap.String("ip", device.IP),
				zap.Duration("retry_after", retryAfter))
			failed++
			continue
		}

		if snmpPollWasFailure(device.Status) {
			if errEv := repo.InsertAvailabilityEvent(device.ID, "available", "SNMP опрос восстановлен"); errEv != nil {
				logger.Warn("availability event insert failed", zap.Int("device_id", device.ID), zap.Error(errEv))
			}
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
		pollBackoff.onSuccess(device.IP)
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
		zap.Int("skipped_backoff", skipped),
		zap.Int("total", len(devices)))

	return success, failed, nil
}

func getValue(result map[string]string, oid string) string {
	if val, ok := result[oid]; ok {
		return val
	}
	return "N/A"
}

func snmpPollWasOK(status string) bool {
	s := strings.TrimSpace(strings.ToLower(status))
	return s == "" || s == "active"
}

func snmpPollWasFailure(status string) bool {
	s := strings.TrimSpace(status)
	return strings.HasPrefix(s, "failed")
}
