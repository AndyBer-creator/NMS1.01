package http

import (
	"net/http"
	"strconv"
	"strings"

	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

type workerPollPanelVM struct {
	Admin       bool
	IntervalSec int
	MinSec      int
	MaxSec      int
	Saved       bool
	ErrMsg      string
}

// WorkerPollSettingsPanel — HTML-фрагмент для дашборда (интервал SNMP-опроса worker).
func (h *Handlers) WorkerPollSettingsPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	sec := h.repo.GetWorkerPollIntervalSeconds(r.Context())
	vm := workerPollPanelVM{
		Admin:       admin,
		IntervalSec: sec,
		MinSec:      postgres.MinWorkerPollIntervalSeconds,
		MaxSec:      postgres.MaxWorkerPollIntervalSeconds,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "workerPollPanel", vm)
}

// SetWorkerPollInterval — POST (HTMX), только admin (RequireAdmin).
func (h *Handlers) SetWorkerPollInterval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}
	raw := strings.TrimSpace(r.FormValue("interval_sec"))
	n, err := strconv.Atoi(raw)
	vm := workerPollPanelVM{
		Admin:       true,
		IntervalSec: h.repo.GetWorkerPollIntervalSeconds(r.Context()),
		MinSec:      postgres.MinWorkerPollIntervalSeconds,
		MaxSec:      postgres.MaxWorkerPollIntervalSeconds,
	}
	if err != nil || raw == "" {
		vm.ErrMsg = "Укажите целое число секунд."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "workerPollPanel", vm)
		return
	}
	if err := h.repo.SetWorkerPollIntervalSeconds(r.Context(), n); err != nil {
		h.logger.Error("SetWorkerPollInterval", zap.Error(err))
		vm.ErrMsg = "Не удалось сохранить: " + err.Error()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "workerPollPanel", vm)
		return
	}
	vm.IntervalSec = h.repo.GetWorkerPollIntervalSeconds(r.Context())
	vm.Saved = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "workerPollPanel", vm)
}
