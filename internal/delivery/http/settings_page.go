package http

import "net/http"

// SettingsPage renders dedicated UI page for runtime/secret settings.
func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "settingsPage", map[string]any{
		"Admin":     admin,
		"CSRFToken": csrfTokenFromContext(r),
		"CSPNonce":  cspNonceFromContext(r),
	})
}
