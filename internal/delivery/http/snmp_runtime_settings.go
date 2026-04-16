package http

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

type snmpRuntimePanelVM struct {
	Admin         bool
	TimeoutSec    int
	Retries       int
	WorstCaseSec  int
	MinTimeoutSec int
	MaxTimeoutSec int
	MinRetries    int
	MaxRetries    int
	Saved         bool
	ErrMsg        string
}

func (h *Handlers) snmpRuntimePanelVM(ctx context.Context, admin bool) snmpRuntimePanelVM {
	current := h.snmp.Config()
	timeoutSec := int(current.Timeout / time.Second)
	if timeoutSec <= 0 {
		timeoutSec = postgres.DefaultSNMPTimeoutSeconds
	}
	effectiveTimeoutSec := h.repo.GetSNMPTimeoutSeconds(ctx, timeoutSec)
	effectiveRetries := h.repo.GetSNMPRetries(ctx, current.Retries)
	return snmpRuntimePanelVM{
		Admin:         admin,
		TimeoutSec:    effectiveTimeoutSec,
		Retries:       effectiveRetries,
		WorstCaseSec:  effectiveTimeoutSec * (effectiveRetries + 1),
		MinTimeoutSec: postgres.MinSNMPTimeoutSeconds,
		MaxTimeoutSec: postgres.MaxSNMPTimeoutSeconds,
		MinRetries:    postgres.MinSNMPRetries,
		MaxRetries:    postgres.MaxSNMPRetries,
	}
}

func (h *Handlers) SNMPRuntimeSettingsPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	vm := h.snmpRuntimePanelVM(r.Context(), admin)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "snmpRuntimePanel", vm)
}

func (h *Handlers) SetSNMPRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}
	vm := h.snmpRuntimePanelVM(r.Context(), true)
	timeoutSec, errTimeout := strconv.Atoi(strings.TrimSpace(r.FormValue("timeout_sec")))
	retries, errRetries := strconv.Atoi(strings.TrimSpace(r.FormValue("retries")))
	if errTimeout != nil || errRetries != nil {
		vm.ErrMsg = "Укажите целые значения timeout и retries."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "snmpRuntimePanel", vm)
		return
	}
	if err := h.repo.SetSNMPTimeoutSeconds(r.Context(), timeoutSec); err != nil {
		h.logger.Error("SetSNMPTimeoutSeconds", zap.Error(err))
		vm.ErrMsg = "Не удалось сохранить timeout: " + err.Error()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "snmpRuntimePanel", vm)
		return
	}
	if err := h.repo.SetSNMPRetries(r.Context(), retries); err != nil {
		h.logger.Error("SetSNMPRetries", zap.Error(err))
		vm.ErrMsg = "Не удалось сохранить retries: " + err.Error()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "snmpRuntimePanel", vm)
		return
	}
	h.syncSNMPRuntimeConfig(r.Context())
	vm = h.snmpRuntimePanelVM(r.Context(), true)
	vm.Saved = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "snmpRuntimePanel", vm)
}
