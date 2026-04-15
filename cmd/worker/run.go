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

func envDurationOrDefault(name string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

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

func escalationAckTimeout() time.Duration {
	return envDurationOrDefault("NMS_INCIDENT_ESCALATION_ACK_TIMEOUT", 0)
}

func escalationCheckInterval() time.Duration {
	return envDurationOrDefault("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL", time.Minute)
}

func escalationTargetAssignee() string {
	return strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ASSIGNEE_ESCALATION"))
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
		olderThan:        envDurationOrDefault("NMS_INCIDENT_ESCALATION_CRITICAL_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_CRITICAL_ASSIGNEE")),
		severity:         "critical",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 critical: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.trap",
		olderThan:        envDurationOrDefault("NMS_INCIDENT_ESCALATION_TRAP_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_TRAP_ASSIGNEE")),
		source:           "trap",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 trap: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.polling",
		olderThan:        envDurationOrDefault("NMS_INCIDENT_ESCALATION_POLLING_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_POLLING_ASSIGNEE")),
		source:           "polling",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 polling: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:             "stage1.manual",
		olderThan:        envDurationOrDefault("NMS_INCIDENT_ESCALATION_MANUAL_ACK_TIMEOUT", 0),
		targetAssignee:   strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_MANUAL_ASSIGNEE")),
		source:           "manual",
		onlyIfUnassigned: true,
		comment:          "auto-escalated stage1 manual: no ack within timeout",
	})
	policies = appendEscalationPolicy(policies, incidentEscalationPolicy{
		name:           "stage2.default",
		olderThan:      envDurationOrDefault("NMS_INCIDENT_ESCALATION_STAGE2_ACK_TIMEOUT", 0),
		targetAssignee: strings.TrimSpace(config.EnvOrFile("NMS_INCIDENT_ESCALATION_STAGE2_ASSIGNEE")),
		comment:        "auto-escalated stage2: no ack within extended timeout",
	})
	return policies
}

// workerOpts задаёт адрес HTTP /metrics; пустая строка — дефолт ":8081".
type workerOpts struct {
	metricsAddr string
}

func (o workerOpts) metricsListenAddr() string {
	if o.metricsAddr == "" {
		return ":8081"
	}
	return o.metricsAddr
}

// run выполняет циклы SNMP-опроса и LLDP до отмены ctx.
func run(ctx context.Context, cfg *config.Config, log *zap.Logger, opts workerOpts) error {
	var metricsShutdown func()
	if srv, _, err := startMetricsHTTPServer(opts.metricsListenAddr(), log); err != nil {
		log.Warn("Metrics server failed to start", zap.Error(err))
	} else {
		metricsShutdown = func() {
			shutdownCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
			defer c()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Warn("metrics shutdown", zap.Error(err))
			}
		}
	}
	if metricsShutdown != nil {
		defer metricsShutdown()
	}

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

	var snmpWg sync.WaitGroup
	snmpWg.Add(1)
	go func() {
		defer snmpWg.Done()
		for {
			if loopCtx.Err() != nil {
				return
			}
			intervalSec := repo.GetWorkerPollIntervalSeconds()
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
			return nil
		case <-lldpTicker.C:
			if !lldpBusy.CompareAndSwap(false, true) {
				log.Warn("LLDP scan skipped: previous run still in progress")
			} else {
				go func() {
					defer lldpBusy.Store(false)
					log.Info("=== LLDP topology cycle ===", zap.String("interval", "5m"))
					start := time.Now()
					summary, err := lldp.ScanAllDevicesLLDP(ctx, repo, snmpClient, log, lldp.ScanParams{})
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
		ReadHeaderTimeout: envDurationOrDefault("NMS_WORKER_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       envDurationOrDefault("NMS_WORKER_HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      envDurationOrDefault("NMS_WORKER_HTTP_WRITE_TIMEOUT", 20*time.Second),
		IdleTimeout:       envDurationOrDefault("NMS_WORKER_HTTP_IDLE_TIMEOUT", 60*time.Second),
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
