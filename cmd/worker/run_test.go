package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"NMS1/internal/config"

	"go.uber.org/zap"
)

func testWorkerConfig(dsn string) *config.Config {
	cfg := &config.Config{}
	cfg.DB.DSN = dsn
	cfg.SNMP.Port = 161
	cfg.SNMP.Timeout = 1
	cfg.SNMP.Retries = 1
	return cfg
}

func junkWorkerDSN() string {
	return "host=127.0.0.1 port=59997 user=u password=p dbname=n sslmode=disable"
}

func TestStartMetricsHTTPServer_Metrics(t *testing.T) {
	log := zap.NewNop()
	srv, addr, err := startMetricsHTTPServer("127.0.0.1:0", log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(ctx)
	}()

	url := fmt.Sprintf("http://%s/metrics", addr)
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	if len(b) < 10 {
		t.Fatalf("short body: %q", b)
	}
}

func TestRun_CancelBeforeFirstPoll(t *testing.T) {
	cfg := testWorkerConfig(junkWorkerDSN())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := run(ctx, cfg, zap.NewNop(), workerOpts{metricsAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("expected nil on shutdown, got %v", err)
	}
}

func TestRun_InvalidMetricsAddrFailsFast(t *testing.T) {
	cfg := testWorkerConfig(junkWorkerDSN())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := run(ctx, cfg, zap.NewNop(), workerOpts{metricsAddr: "127.0.0.1:99999"})
	if err == nil {
		t.Fatal("expected metrics server startup error")
	}
	if !strings.Contains(err.Error(), "metrics server failed to start") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEscalationEnvConfig(t *testing.T) {
	t.Setenv("NMS_INCIDENT_ESCALATION_ACK_TIMEOUT", "15m")
	t.Setenv("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL", "45s")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_ESCALATION", "  noc-l3  ")
	if got := escalationAckTimeout(); got != 15*time.Minute {
		t.Fatalf("ack timeout got %s", got)
	}
	if got := escalationCheckInterval(); got != 45*time.Second {
		t.Fatalf("check interval got %s", got)
	}
	if got := escalationTargetAssignee(); got != "noc-l3" {
		t.Fatalf("target assignee got %q", got)
	}

	_ = os.Unsetenv("NMS_INCIDENT_ESCALATION_ACK_TIMEOUT")
	_ = os.Unsetenv("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL")
	_ = os.Unsetenv("NMS_INCIDENT_ASSIGNEE_ESCALATION")
	if got := escalationAckTimeout(); got != 0 {
		t.Fatalf("default ack timeout got %s", got)
	}
	if got := escalationCheckInterval(); got != time.Minute {
		t.Fatalf("default check interval got %s", got)
	}
	if got := escalationTargetAssignee(); got != "" {
		t.Fatalf("default assignee got %q", got)
	}
}

func TestLoadEscalationPoliciesV2(t *testing.T) {
	t.Setenv("NMS_INCIDENT_ESCALATION_ACK_TIMEOUT", "10m")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_ESCALATION", "noc-l2")
	t.Setenv("NMS_INCIDENT_ESCALATION_STAGE2_ACK_TIMEOUT", "30m")
	t.Setenv("NMS_INCIDENT_ESCALATION_STAGE2_ASSIGNEE", "noc-l3")
	t.Setenv("NMS_INCIDENT_ESCALATION_CRITICAL_ACK_TIMEOUT", "5m")
	t.Setenv("NMS_INCIDENT_ESCALATION_CRITICAL_ASSIGNEE", "sev1-oncall")

	policies := loadEscalationPoliciesV2()
	if len(policies) < 3 {
		t.Fatalf("expected at least 3 policies, got %d", len(policies))
	}
	if policies[0].name != "stage1.default" || !policies[0].onlyIfUnassigned {
		t.Fatalf("unexpected first policy: %+v", policies[0])
	}
	var hasStage2 bool
	var hasCritical bool
	for _, p := range policies {
		if p.name == "stage2.default" && p.targetAssignee == "noc-l3" && !p.onlyIfUnassigned {
			hasStage2 = true
		}
		if p.name == "stage1.critical" && p.severity == "critical" && p.targetAssignee == "sev1-oncall" {
			hasCritical = true
		}
	}
	if !hasStage2 {
		t.Fatal("stage2.default policy missing")
	}
	if !hasCritical {
		t.Fatal("stage1.critical policy missing")
	}
}
