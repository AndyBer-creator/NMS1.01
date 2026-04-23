package http

import (
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type externalHealthStatus struct {
	Grafana    string
	Prometheus string
}

func parseURLOrEmpty(raw string) *url.URL {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	return u
}

func (h *Handlers) probeExternalEndpoint(ctx context.Context, rawURL string) string {
	u := parseURLOrEmpty(rawURL)
	if u == nil {
		return "not_configured"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "down"
	}
	client := h.httpClient
	if client == nil {
		client = &http.Client{Timeout: 1200 * time.Millisecond}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "down"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return "up"
	}
	return "degraded"
}

func (h *Handlers) dashboardExternalHealth(ctx context.Context, admin bool) externalHealthStatus {
	if !admin {
		return externalHealthStatus{}
	}
	grafanaURL := strings.TrimSpace(h.repo.GetStringSetting(ctx, postgres.SettingKeyGrafanaBaseURL))
	if grafanaURL == "" {
		grafanaURL = strings.TrimSpace(config.EnvOrFile("NMS_GRAFANA_BASE_URL"))
	}
	prometheusURL := strings.TrimSpace(h.repo.GetStringSetting(ctx, postgres.SettingKeyPrometheusBaseURL))
	if prometheusURL == "" {
		prometheusURL = strings.TrimSpace(config.EnvOrFile("NMS_PROMETHEUS_BASE_URL"))
	}
	if prometheusURL == "" {
		prometheusURL = strings.TrimSpace(config.EnvOrFile("PROMETHEUS_BASE_URL"))
	}
	grafanaCtx, cancelGrafana := context.WithTimeout(ctx, 900*time.Millisecond)
	grafanaStatus := h.probeExternalEndpoint(grafanaCtx, grafanaURL)
	cancelGrafana()
	promCtx, cancelProm := context.WithTimeout(ctx, 900*time.Millisecond)
	promStatus := h.probeExternalEndpoint(promCtx, prometheusURL)
	cancelProm()
	return externalHealthStatus{
		Grafana:    grafanaStatus,
		Prometheus: promStatus,
	}
}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	extHealth := h.dashboardExternalHealth(r.Context(), admin)
	_ = h.dashboardTmpl.Execute(w, map[string]any{
		"Admin":                 admin,
		"CSRFToken":             csrfTokenFromContext(r),
		"CSPNonce":              cspNonceFromContext(r),
		"GrafanaIncidentSLAURL": grafanaIncidentSLAURL(),
		"GrafanaHealth":         extHealth.Grafana,
		"PrometheusHealth":      extHealth.Prometheus,
	})
}
