package http

import (
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

type runtimeSettingsPanelVM struct {
	Admin             bool
	SMTPHost          string
	SMTPPort          string
	SMTPFrom          string
	GrafanaBaseURL    string
	PrometheusBaseURL string
	Saved             bool
	Err               string
}

func (h *Handlers) RuntimeSettingsPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	vm := runtimeSettingsPanelVM{
		Admin:             admin,
		SMTPHost:          h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPHost),
		SMTPPort:          h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPPort),
		SMTPFrom:          h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPFrom),
		GrafanaBaseURL:    h.repo.GetStringSetting(r.Context(), postgres.SettingKeyGrafanaBaseURL),
		PrometheusBaseURL: h.repo.GetStringSetting(r.Context(), postgres.SettingKeyPrometheusBaseURL),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "runtimeSettingsPanel", vm)
}

func (h *Handlers) SetRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}
	vm := runtimeSettingsPanelVM{
		Admin:             true,
		SMTPHost:          strings.TrimSpace(r.FormValue("smtp_host")),
		SMTPPort:          strings.TrimSpace(r.FormValue("smtp_port")),
		SMTPFrom:          strings.TrimSpace(r.FormValue("smtp_from")),
		GrafanaBaseURL:    strings.TrimSpace(r.FormValue("grafana_base_url")),
		PrometheusBaseURL: strings.TrimSpace(r.FormValue("prometheus_base_url")),
	}
	if vm.SMTPHost != "" && strings.Contains(vm.SMTPHost, "://") {
		vm.Err = "SMTP host must be a hostname or IP, not URL."
	} else if vm.SMTPPort != "" {
		p, err := strconv.Atoi(vm.SMTPPort)
		if err != nil || p < 1 || p > 65535 {
			vm.Err = "SMTP port must be an integer in range 1..65535."
		}
	}
	if vm.Err == "" && vm.SMTPFrom != "" {
		if _, err := mail.ParseAddress(vm.SMTPFrom); err != nil {
			vm.Err = "SMTP from must be a valid email."
		}
	}
	if vm.Err == "" && vm.GrafanaBaseURL != "" {
		if _, err := url.ParseRequestURI(vm.GrafanaBaseURL); err != nil {
			vm.Err = "Grafana base URL must be a valid absolute URL."
		}
	}
	if vm.Err == "" && vm.PrometheusBaseURL != "" {
		if _, err := url.ParseRequestURI(vm.PrometheusBaseURL); err != nil {
			vm.Err = "Prometheus base URL must be a valid absolute URL."
		}
	}
	if vm.Err != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "runtimeSettingsPanel", vm)
		return
	}
	if err := h.repo.SetStringSetting(r.Context(), postgres.SettingKeySMTPHost, vm.SMTPHost); err != nil {
		h.logger.Error("SetRuntimeSettings SMTP host", zap.Error(err))
		vm.Err = "Failed to save SMTP host: " + err.Error()
	}
	if vm.Err == "" {
		if err := h.repo.SetStringSetting(r.Context(), postgres.SettingKeySMTPPort, vm.SMTPPort); err != nil {
			h.logger.Error("SetRuntimeSettings SMTP port", zap.Error(err))
			vm.Err = "Failed to save SMTP port: " + err.Error()
		}
	}
	if vm.Err == "" {
		if err := h.repo.SetStringSetting(r.Context(), postgres.SettingKeySMTPFrom, vm.SMTPFrom); err != nil {
			h.logger.Error("SetRuntimeSettings SMTP from", zap.Error(err))
			vm.Err = "Failed to save SMTP from: " + err.Error()
		}
	}
	if vm.Err == "" {
		if err := h.repo.SetStringSetting(r.Context(), postgres.SettingKeyGrafanaBaseURL, vm.GrafanaBaseURL); err != nil {
			h.logger.Error("SetRuntimeSettings grafana URL", zap.Error(err))
			vm.Err = "Failed to save Grafana URL: " + err.Error()
		}
	}
	if vm.Err == "" {
		if err := h.repo.SetStringSetting(r.Context(), postgres.SettingKeyPrometheusBaseURL, vm.PrometheusBaseURL); err != nil {
			h.logger.Error("SetRuntimeSettings prometheus URL", zap.Error(err))
			vm.Err = "Failed to save Prometheus URL: " + err.Error()
		}
	}
	if vm.Err == "" {
		vm.Saved = true
		changedBy := "unknown"
		if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
			changedBy = strings.TrimSpace(u.username)
		}
		h.logger.Info("runtime settings updated",
			zap.String("changed_by", changedBy),
			zap.String("smtp_host", vm.SMTPHost),
			zap.String("smtp_port", vm.SMTPPort),
			zap.String("smtp_from", vm.SMTPFrom),
			zap.String("grafana_base_url", vm.GrafanaBaseURL),
			zap.String("prometheus_base_url", vm.PrometheusBaseURL))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "runtimeSettingsPanel", vm)
}
