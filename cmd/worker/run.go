package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/usecases/lldp"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

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

	lldpTicker := time.NewTicker(5 * time.Minute)
	defer lldpTicker.Stop()

	log.Info("🚀 NMS Worker v3 started (prod logging)")

	for {
		select {
		case <-ctx.Done():
			log.Info("🛑 Worker shutdown")
			cancelLoop()
			snmpWg.Wait()
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
	srv := &http.Server{Handler: mux}

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
