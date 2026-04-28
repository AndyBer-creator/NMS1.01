package http

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"NMS1/internal/config"

	"go.uber.org/zap"
)

type escalationPolicyView struct {
	Name             string `json:"name"`
	OlderThan        string `json:"older_than"`
	TargetAssignee   string `json:"target_assignee"`
	Severity         string `json:"severity,omitempty"`
	Source           string `json:"source,omitempty"`
	OnlyIfUnassigned bool   `json:"only_if_unassigned"`
	Active           bool   `json:"active"`
}

type incidentAutomationPanelVM struct {
	Admin                 bool
	CSPNonce              string
	EscalationIntervalSec int
	Policies              []escalationPolicyView
	PolicyWarnings        []string
	ITSMMappingCount      int
	ITSMMappingsEnabled   bool
}

func envDuration(name string) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

func envDurationWithDefault(name string, fallback time.Duration) time.Duration {
	if d := envDuration(name); d > 0 {
		return d
	}
	return fallback
}

func escalationPoliciesForPanel() ([]escalationPolicyView, []string) {
	type row struct {
		name             string
		timeoutEnv       string
		assigneeEnv      string
		fallbackTimeout  time.Duration
		severity         string
		source           string
		onlyIfUnassigned bool
	}
	rows := []row{
		{name: "stage1.default", timeoutEnv: "NMS_INCIDENT_ESCALATION_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ASSIGNEE_ESCALATION", onlyIfUnassigned: true},
		{name: "stage1.critical", timeoutEnv: "NMS_INCIDENT_ESCALATION_CRITICAL_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ESCALATION_CRITICAL_ASSIGNEE", severity: "critical", onlyIfUnassigned: true},
		{name: "stage1.trap", timeoutEnv: "NMS_INCIDENT_ESCALATION_TRAP_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ESCALATION_TRAP_ASSIGNEE", source: "trap", onlyIfUnassigned: true},
		{name: "stage1.polling", timeoutEnv: "NMS_INCIDENT_ESCALATION_POLLING_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ESCALATION_POLLING_ASSIGNEE", source: "polling", onlyIfUnassigned: true},
		{name: "stage1.manual", timeoutEnv: "NMS_INCIDENT_ESCALATION_MANUAL_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ESCALATION_MANUAL_ASSIGNEE", source: "manual", onlyIfUnassigned: true},
		{name: "stage2.default", timeoutEnv: "NMS_INCIDENT_ESCALATION_STAGE2_ACK_TIMEOUT", assigneeEnv: "NMS_INCIDENT_ESCALATION_STAGE2_ASSIGNEE", onlyIfUnassigned: false},
	}
	out := make([]escalationPolicyView, 0, len(rows))
	warnings := make([]string, 0, len(rows))
	for _, p := range rows {
		timeout := envDurationWithDefault(p.timeoutEnv, p.fallbackTimeout)
		assignee := strings.TrimSpace(config.EnvOrFile(p.assigneeEnv))
		active := timeout > 0 && assignee != ""
		if timeout > 0 && assignee == "" {
			warnings = append(warnings, p.name+": timeout set but assignee is empty")
		}
		if timeout == 0 && assignee != "" {
			warnings = append(warnings, p.name+": assignee set but timeout is missing/invalid")
		}
		out = append(out, escalationPolicyView{
			Name:             p.name,
			OlderThan:        timeout.String(),
			TargetAssignee:   assignee,
			Severity:         p.severity,
			Source:           p.source,
			OnlyIfUnassigned: p.onlyIfUnassigned,
			Active:           active,
		})
	}
	return out, warnings
}

func (h *Handlers) IncidentAutomationPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	policies, warnings := escalationPoliciesForPanel()
	interval := envDurationWithDefault("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL", time.Minute)
	enabled := true
	mappings, err := h.repo.ListITSMInboundMappings(r.Context(), "", &enabled)
	if err != nil {
		h.logger.Error("ListITSMInboundMappings failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	vm := incidentAutomationPanelVM{
		Admin:                 admin,
		CSPNonce:              cspNonceFromContext(r),
		EscalationIntervalSec: int(interval.Seconds()),
		Policies:              policies,
		PolicyWarnings:        warnings,
		ITSMMappingCount:      len(mappings),
		ITSMMappingsEnabled:   len(mappings) > 0,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "incidentAutomationPanel", vm)
}

func (h *Handlers) IncidentAutomationSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	policies, warnings := escalationPoliciesForPanel()
	interval := envDurationWithDefault("NMS_INCIDENT_ESCALATION_CHECK_INTERVAL", time.Minute)
	enabled := true
	items, err := h.repo.ListITSMInboundMappings(r.Context(), "", &enabled)
	if err != nil {
		h.logger.Error("ListITSMInboundMappings snapshot failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"escalation": map[string]any{
			"check_interval_sec": int(interval.Seconds()),
			"policies":           policies,
			"warnings":           warnings,
		},
		"itsm_inbound_mappings": items,
		"counts": map[string]int{
			"active_escalation_policies": func() int {
				n := 0
				for _, p := range policies {
					if p.Active {
						n++
					}
				}
				return n
			}(),
			"active_itsm_mapping_rules": len(items),
		},
	})
}
