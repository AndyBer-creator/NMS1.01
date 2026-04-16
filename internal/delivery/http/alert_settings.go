package http

import (
	"net/http"
	"net/mail"
	"strings"

	"go.uber.org/zap"
)

type alertEmailPanelVM struct {
	Admin bool
	Email string
	Saved bool
	Err   string
}

func (h *Handlers) AlertEmailPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	vm := alertEmailPanelVM{
		Admin: admin,
		Email: h.repo.GetAlertEmailTo(r.Context()),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "alertEmailPanel", vm)
}

func (h *Handlers) SetAlertEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	vm := alertEmailPanelVM{Admin: true, Email: email}
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			vm.Err = "Неверный формат email."
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = h.devicesTmpl.ExecuteTemplate(w, "alertEmailPanel", vm)
			return
		}
	}
	if err := h.repo.SetAlertEmailTo(r.Context(), email); err != nil {
		h.logger.Error("SetAlertEmail", zap.Error(err))
		vm.Err = "Не удалось сохранить: " + err.Error()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.devicesTmpl.ExecuteTemplate(w, "alertEmailPanel", vm)
		return
	}
	vm.Saved = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "alertEmailPanel", vm)
}
