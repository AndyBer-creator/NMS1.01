package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/usecases/lldp"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// envIntOrDefault parses a positive integer from env or returns fallback.
func envIntOrDefault(name string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// escalationAckTimeout returns stage-1 incident escalation timeout.
func escalationAckTimeout() time.Duration {
	return config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_ACK_TIMEOUT", 0)
}

// escalationCheckInterval returns periodic interval for escalation checks.
func escalationCheckInterval() time.Duration {
	return config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL", time.Minute)
}

// escalationTargetAssignee returns default stage-1 escalation assignee.
func escalationTargetAssignee() string {
	return strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_ESCALATION"))
}

// metricsRetentionMonths returns metrics partition retention in months.
func metricsRetentionMonths() int {
	return envIntOrDefault("NMS_METRICS_RETENTION_MONTHS", 6)
}

// metricsRetentionCheckInterval returns prune interval for old metric partitions.
func metricsRetentionCheckInterval() time.Duration {
	return config.EnvDurationOrDefault("NMS_METRICS_RETENTION_CHECK_INTERVAL", 24*time.Hour)
}

type incidentEscalationPolicy struct {
	name             string
	olderThan        time.Duration
	targetAssignee   string
	severity         string
	source           string
	onlyIfUnassigned bool
	comment          string
}

// appendEscalationPolicy validates and appends an escalation policy.
func appendEscalationPolicy(out []incidentEscalationPolicy, p incidentEscalationPolicy) []incidentEscalationPolicy {
	if p.olderThan <= 0 || strings.TrimSpace(p.targetAssignee) == "" {
		return out
	}
	p.targetAssignee = strings.TrimSpace(p.targetAssignee)
	if strings.TrimSpace(p.comment) == "" {
		p.comment = "auto-escalated: no ack within timeout"
	}
	return append(out, p)
}

// loadEscalationPoliciesV2 builds active escalation policies from runtime env.
func loadEscalationPoliciesV2() []incidentEscalationPolicy {
	policies := make([]incidentEscalationPolicy, 0, 8)
	stage1Timeout := escalationAckTimeout()
	stage1Assignee := escalationTargetAssignee()
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.default",
		olderThan:        stage1Timeout,
		targetAssignee:   stage1Assignee,
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.critical",
		olderThan:        config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_CRITICAL_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_CRITICAL_ASSIGNEE")),
		severity:         "critical",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 critical: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.trap",
		olderThan:        config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_TRAP_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_TRAP_ASSIGNEE")),
		source:           "trap",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 trap: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.polling",
		olderThan:        config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_POLLING_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_POLLING_ASSIGNEE")),
		source:           "polling",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 polling: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.manual",
		olderThan:        config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_MANUAL_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_MANUAL_ASSIGNEE")),
		source:           "manual",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 manual: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:           "stage2.default",
		olderThan:      config.EnvDurationOrDefault("NMS_INCIDENT_ESCALATION_STAGE2_ACK_TIMEOUT", 0),
		targetAssignee: strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_STAGE2_ASSIGNEE")),
		comment:        "auto-escalated stage2: no ack within extended timeout",
	})
	return policies
}

// workerOpts задаёт адрес HTTP /metrics; пустая строка — дефолт ":8081".
type workerOpts struct {
	metricsAddr string
}

// metricsListenAddr returns explicit metrics address or a default one.
func (o workerOpts) metricsListenAddr() string {
	if o.metricsAddr == "" {
		return ":8081"
	}
	return o.metricsAddr
}

// run выполняет циклы SNMP-опроса и LLDP до отмены ctx.
func run(ctx context.Context, cfg *config.Config, log *zap.Logger, opts workerOpts) error {
	srv, _, err := startMetricsHTTPServer(opts.metricsListenAddr(), log)
	if err != nil {
		return fmt.Errorf("metrics server failed to start: %w", err)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("metrics shutdown", zap.Error(err))
		}
	}()

	repo, err := postgres.New(cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer func() { _ = repo.Close() }()

	snmpClient := snmp.New(
		int(cfg.SNMP.Port),
		time.Duration(cfg.SNMP.Timeout)*time.Second,
		cfg.SNMP.Retries,
	)

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()
	refreshSNMPRuntimeConfig := func() {
		current := snmpClient.Config()
		fallbackTimeoutSec := int(current.Timeout / time.Second)
		if fallbackTimeoutSec <= 0 {
			fallbackTimeoutSec = postgres.DefaultSNMPTimeoutSeconds
		}
		timeoutSec := repo.GetSNMPTimeoutSeconds(loopCtx, fallbackTimeoutSec)
		retries := repo.GetSNMPRetries(loopCtx, current.Retries)
		if current.Timeout == time.Duration(timeoutSec)*time.Second && current.Retries == retries {
			return
		}
		snmpClient.ApplyRuntimeConfig(time.Duration(timeoutSec)*time.Second, retries)
		log.Info("SNMP runtime config updated", zap.Int("timeout_sec", timeoutSec), zap.Int("retries", retries))
	}

	var snmpWg sync.WaitGroup
	snmpWg.Add(1)
	go func() {
		defer snmpWg.Done()
		for {
			if loopCtx.Err() != nil {
				return
			}
			intervalSec := repo.GetWorkerPollIntervalSeconds(loopCtx)
			log.Info("SNMP poll: пауза до следующего цикла", zap.Int("interval_sec", intervalSec))
			timer := time.NewTimer(time.Duration(intervalSec) * time.Second)
			select {
			case <-loopCtx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			if loopCtx.Err() != nil {
				return
			}
			refreshSNMPRuntimeConfig()
			log.Info("=== Polling cycle ===", zap.Int("interval_sec", intervalSec))
			start := time.Now()
			success, failed, err := pollAllDevices(loopCtx, repo, snmpClient, log)
			workerPollDurationSeconds.Observe(time.Since(start).Seconds())
			if success > 0 {
				workerPollDevicesTotal.WithLabelValues("active").Add(float64(success))
			}
			if failed > 0 {
				workerPollDevicesTotal.WithLabelValues("failed").Add(float64(failed))
			}
			if err != nil {
				log.Error("Polling failed", zap.Error(err))
			}
			log.Info("=== Cycle complete ===", zap.Duration("duration", time.Since(start)))
		}
	}()

	var escalationWg sync.WaitGroup
	escalationInterval := escalationCheckInterval()
	policies := loadEscalationPoliciesV2()
	stage1Timeout := escalationAckTimeout()
	incidentEscalationAckTimeoutSeconds.Set(stage1Timeout.Seconds())
	if len(policies) > 0 {
		log.Info("Incident escalation enabled",
			zap.Duration("check_interval", escalationInterval),
			zap.Int("policies", len(policies)))
		escalationWg.Add(1)
		go func() {
			defer escalationWg.Done()
			ticker := time.NewTicker(escalationInterval)
			defer ticker.Stop()
			for {
				select {
				case <-loopCtx.Done():
					return
				case <-ticker.C:
					for _, p := range policies {
						n, err := repo.EscalateUnackedIncidentsWithFilter(
							loopCtx,
							p.olderThan,
							p.targetAssignee,
							"system-escalation",
							p.comment,
							p.severity,
							p.source,
							p.onlyIfUnassigned,
						)
						if err != nil {
							log.Warn("Incident escalation check failed",
								zap.String("policy", p.name),
								zap.Error(err))
							continue
						}
						if n > 0 {
							incidentEscalationsTotal.Add(float64(n))
							incidentEscalationsByPolicyTotal.WithLabelValues(p.name).Add(float64(n))
							log.Info("Incident escalation applied",
								zap.String("policy", p.name),
								zap.Int64("escalated", n))
						}
					}
				}
			}
		}()
	} else {
		log.Info("Incident escalation disabled",
			zap.String("reason", "no_valid_policies"))
	}

	var metricsRetentionWg sync.WaitGroup
	retainMonths := metricsRetentionMonths()
	retentionInterval := metricsRetentionCheckInterval()
	if retainMonths > 0 {
		log.Info("Metrics retention enabled",
			zap.Int("retain_months", retainMonths),
			zap.Duration("check_interval", retentionInterval))
		metricsRetentionWg.Add(1)
		go func() {
			defer metricsRetentionWg.Done()
			ticker := time.NewTicker(retentionInterval)
			defer ticker.Stop()

			runPrune := func() {
				dropped, err := repo.PruneOldMetricPartitions(loopCtx, retainMonths)
				if err != nil {
					log.Warn("Metrics retention prune failed", zap.Error(err))
					return
				}
				if dropped > 0 {
					log.Info("Metrics retention prune completed", zap.Int("dropped_partitions", dropped))
				}
			}

			runPrune()
			for {
				select {
				case <-loopCtx.Done():
					return
				case <-ticker.C:
					runPrune()
				}
			}
		}()
	}

	var lldpWg sync.WaitGroup

	lldpTicker := time.NewTicker(5 * time.Minute)
	defer lldpTicker.Stop()

	log.Info("🚀 NMS Worker v3 started (prod logging)")

	for {
		select {
		case <-ctx.Done():
			log.Info("🛑 Worker shutdown")
			cancelLoop()
			snmpWg.Wait()
			escalationWg.Wait()
			metricsRetentionWg.Wait()
			lldpWg.Wait()
			return nil
		case <-lldpTicker.C:
			if !lldpBusy.CompareAndSwap(false, true) {
				log.Warn("LLDP scan skipped: previous run still in progress")
			} else {
				lldpWg.Add(1)
				go func() {
					defer lldpWg.Done()
					defer lldpBusy.Store(false)
					refreshSNMPRuntimeConfig()
					log.Info("=== LLDP topology cycle ===", zap.String("interval", "5m"))
					start := time.Now()
					summary, err := lldp.ScanAllDevicesLLDP(loopCtx, repo, snmpClient, log, lldp.ScanParams{})
					lldpScanDurationSeconds.Observe(time.Since(start).Seconds())
					if summary != nil {
						lldpLinksFoundGauge.Set(float64(summary.LinksFound))
						lldpLinksInsertedGauge.Set(float64(summary.LinksInserted))
						lldpLinksInsertedTotal.Add(float64(summary.LinksInserted))
					}
					if err != nil {
						log.Error("LLDP scan failed", zap.Error(err))
					}
					log.Info("=== LLDP cycle complete ===", zap.Duration("duration", time.Since(start)))
				}()
			}
		}
	}
}

// startMetricsHTTPServer поднимает /metrics на TCP; вызывающий обязан вызвать Shutdown на *http.Server.
func startMetricsHTTPServer(addr string, log *zap.Logger) (*http.Server, net.Addr, error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: config.EnvDurationOrDefault("NMS_WORKER_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       config.EnvDurationOrDefault("NMS_WORKER_HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      config.EnvDurationOrDefault("NMS_WORKER_HTTP_WRITE_TIMEOUT", 20*time.Second),
		IdleTimeout:       config.EnvDurationOrDefault("NMS_WORKER_HTTP_IDLE_TIMEOUT", 60*time.Second),
		MaxHeaderBytes:    envIntOrDefault("NMS_WORKER_HTTP_MAX_HEADER_BYTES", 1<<20), // 1 MiB
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	go func() {
		log.Info("Worker metrics server started", zap.String("addr", ln.Addr().String()))
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("Worker metrics server error", zap.Error(err))
		}
	}()
	return srv, ln.Addr(), nil
}
