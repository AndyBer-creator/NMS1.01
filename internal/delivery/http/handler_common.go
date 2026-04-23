package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"

	"github.com/go-chi/chi/v5"
)

func deviceIDFromChi(r *http.Request) (int, error) {
	s := strings.TrimSpace(chi.URLParam(r, "id"))
	if s == "" {
		return 0, fmt.Errorf("empty device id")
	}
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid device id")
	}
	return id, nil
}

func (h *Handlers) syncSNMPRuntimeConfig(ctx context.Context) {
	if h == nil || h.snmp == nil || h.repo == nil {
		return
	}
	current := h.snmp.Config()
	fallbackTimeoutSec := int(current.Timeout / time.Second)
	if fallbackTimeoutSec <= 0 {
		fallbackTimeoutSec = postgres.DefaultSNMPTimeoutSeconds
	}
	timeoutSec := h.repo.GetSNMPTimeoutSeconds(ctx, fallbackTimeoutSec)
	retries := h.repo.GetSNMPRetries(ctx, current.Retries)
	if current.Timeout == time.Duration(timeoutSec)*time.Second && current.Retries == retries {
		return
	}
	h.snmp.ApplyRuntimeConfig(time.Duration(timeoutSec)*time.Second, retries)
}

// resolveOIDInput keeps numeric OIDs and resolves symbolic ones through MIB resolver.
func (h *Handlers) resolveOIDInput(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("пустой OID")
	}
	return h.mib.ResolveToNumeric(s)
}

func grafanaIncidentSLAURL() string {
	base := strings.TrimSpace(config.EnvOrFile("NMS_GRAFANA_BASE_URL"))
	if base == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	return base + "/d/nms-incident-sla"
}
