package main

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/domain"
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
	workerPollSkippedBackoffTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nms_worker_poll_skipped_backoff_total",
		Help: "Total devices skipped due to per-device polling backoff",
	})
	workerPollConfigConcurrency = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nms_worker_poll_config_concurrency",
		Help: "Configured worker polling concurrency used in the latest cycle",
	})
	workerPollConfigRateLimitPerSec = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nms_worker_poll_config_rate_limit_per_sec",
		Help: "Configured worker polling start rate limit per second used in the latest cycle",
	})
	incidentEscalationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nms_incident_escalations_total",
		Help: "Total incidents auto-escalated due to ack timeout",
	})
	incidentEscalationAckTimeoutSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nms_incident_escalation_ack_timeout_seconds",
		Help: "Configured ack-timeout threshold for incident auto-escalation",
	})
	incidentEscalationsByPolicyTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nms_incident_escalations_policy_total",
			Help: "Total incidents auto-escalated by escalation policy",
		},
		[]string{"policy"},
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

func pollingIncidentSuppressionWindow() time.Duration {
	raw := strings.TrimSpace(config.EnvOrFile("NMS_POLL_INCIDENT_SUPPRESSION_WINDOW"))
	if raw == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

func pollWorkerConcurrency() int {
	v := strings.TrimSpace(os.Getenv("NMS_WORKER_POLL_CONCURRENCY"))
	if v == "" {
		return 4
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 4
	}
	if n > 128 {
		return 128
	}
	return n
}

func pollRateLimitPerSec() int {
	v := strings.TrimSpace(os.Getenv("NMS_WORKER_POLL_RATE_LIMIT_PER_SEC"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	if n > 1000 {
		return 1000
	}
	return n
}

type pollThrottle struct {
	ch   <-chan struct{}
	stop func()
}

type pollResult int

const (
	pollResultSuccess pollResult = iota
	pollResultFailed
	pollResultSkipped
)

func newPollThrottle(ratePerSec int) pollThrottle {
	if ratePerSec <= 0 {
		return pollThrottle{
			ch:   nil,
			stop: func() {},
		}
	}
	interval := time.Second / time.Duration(ratePerSec)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	ch := make(chan struct{}, 1)
	ch <- struct{}{} // allow one immediate request
	done := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}()
	return pollThrottle{
		ch: ch,
		stop: func() {
			close(done)
			ticker.Stop()
		},
	}
}

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
	prometheus.MustRegister(workerPollSkippedBackoffTotal)
	prometheus.MustRegister(workerPollConfigConcurrency)
	prometheus.MustRegister(workerPollConfigRateLimitPerSec)
	prometheus.MustRegister(incidentEscalationsTotal)
	prometheus.MustRegister(incidentEscalationAckTimeoutSeconds)
	prometheus.MustRegister(incidentEscalationsByPolicyTotal)
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

	devices, err := repo.ListDevices(ctx)
	if err != nil {
		logger.Error("Failed to list devices", zap.Error(err))
		return 0, 0, err
	}

	baseOids := config.StandardOIDs()
	concurrency := pollWorkerConcurrency()
	ratePerSec := pollRateLimitPerSec()
	throttle := newPollThrottle(ratePerSec)
	defer throttle.stop()
	workerPollConfigConcurrency.Set(float64(concurrency))
	workerPollConfigRateLimitPerSec.Set(float64(ratePerSec))

	logger.Info("Starting poll",
		zap.Int("devices", len(devices)),
		zap.Int("oids", len(baseOids)),
		zap.Int("workers", concurrency),
		zap.Int("rate_limit_per_sec", ratePerSec))

	var successN, failedN, skippedN atomic.Int64
	jobs := make(chan *domain.Device, concurrency*2)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for device := range jobs {
				if throttle.ch != nil {
					select {
					case <-ctx.Done():
						return
					case _, ok := <-throttle.ch:
						if !ok {
							return
						}
					}
				}
				switch pollOneDevice(ctx, repo, snmpClient, logger, device, baseOids) {
				case pollResultSuccess:
					successN.Add(1)
				case pollResultSkipped:
					skippedN.Add(1)
				default:
					failedN.Add(1)
				}
			}
		}()
	}

	interrupted := false
	for _, d := range devices {
		select {
		case <-ctx.Done():
			interrupted = true
		case jobs <- d:
		}
		if interrupted {
			break
		}
	}
	close(jobs)
	wg.Wait()

	success = int(successN.Load())
	failed = int(failedN.Load())
	skipped := int(skippedN.Load())

	if interrupted || ctx.Err() != nil {
		logger.Info("Polling interrupted",
			zap.Int("processed", success+failed+skipped))
		return success, failed, ctx.Err()
	}

	logger.Info("Polling stats",
		zap.Int("success", success),
		zap.Int("failed", failed),
		zap.Int("skipped_backoff", skipped),
		zap.Int("total", len(devices)))
	if skipped > 0 {
		workerPollSkippedBackoffTotal.Add(float64(skipped))
	}

	return success, failed, nil
}

func pollOneDevice(
	ctx context.Context,
	repo *postgres.Repo,
	snmpClient *snmp.Client,
	logger *zap.Logger,
	device *domain.Device,
	baseOids []string,
) pollResult {
	now := time.Now()
	if skip, wait := pollBackoff.shouldSkip(device.IP, now); skip {
		logger.Info("Polling skipped by backoff",
			zap.Int("id", device.ID),
			zap.String("ip", device.IP),
			zap.Duration("retry_in", wait))
		return pollResultSkipped
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
			if errEv := repo.InsertAvailabilityEvent(ctx, device.ID, "unavailable", detail); errEv != nil {
				logger.Warn("availability event insert failed", zap.Int("device_id", device.ID), zap.Error(errEv))
			}
			details, _ := json.Marshal(map[string]any{
				"status": status,
				"error":  err.Error(),
				"ip":     device.IP,
				"name":   device.Name,
			})
			_, _, ierr := repo.CreateOrTouchOpenIncident(
				ctx,
				&device.ID,
				"SNMP device unavailable",
				"critical",
				"polling",
				details,
				pollingIncidentSuppressionWindow(),
			)
			if ierr != nil {
				logger.Warn("incident correlate failed", zap.Int("device_id", device.ID), zap.Error(ierr))
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
		return pollResultFailed
	}

	if snmpPollWasFailure(device.Status) {
		if errEv := repo.InsertAvailabilityEvent(ctx, device.ID, "available", "SNMP опрос восстановлен"); errEv != nil {
			logger.Warn("availability event insert failed", zap.Int("device_id", device.ID), zap.Error(errEv))
		}
		if _, errRes := repo.ResolveOpenIncidentsBySource(ctx, device.ID, "polling", "system", "auto-resolved: SNMP poll restored"); errRes != nil {
			logger.Warn("incident auto-resolve failed", zap.Int("device_id", device.ID), zap.Error(errRes))
		}
	}

	metricsSaved := 0
	for oid, value := range result {
		if err := repo.SaveMetric(ctx, device.ID, oid, value); err != nil {
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

	sysDescr := getValue(result, "1.3.6.1.2.1.1.1.0")
	logger.Info("✅ Device polled OK",
		zap.String("ip", device.IP),
		zap.String("sysDescr", sysDescr),
		zap.Int("metrics", len(result)),
		zap.Int("saved", metricsSaved))
	return pollResultSuccess
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
