package http

import (
	"NMS1/internal/config"
	"NMS1/internal/domain"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type itsmInboundRequest struct {
	IncidentID int64  `json:"incident_id"`
	Provider   string `json:"provider,omitempty"`
	Status     string `json:"status,omitempty"`
	Priority   string `json:"priority,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Assignee   string `json:"assignee,omitempty"`
	Comment    string `json:"comment,omitempty"`
	ChangedBy  string `json:"changed_by,omitempty"`
}

type itsmInboundResolved struct {
	Input            itsmInboundRequest
	Provider         string
	Status           string
	Assignee         string
	AppliedMappingID int64
}

func normalizeInboundIncidentStatus(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "":
		return ""
	case "new", "open":
		return "new"
	case "acknowledged", "ack", "acknowledge":
		return "acknowledged"
	case "in_progress", "in-progress", "inprogress", "working":
		return "in_progress"
	case "resolved", "resolve":
		return "resolved"
	case "closed", "close":
		return "closed"
	default:
		return s
	}
}

func inboundITSMToken() string {
	return strings.TrimSpace(config.EnvOrFile("NMS_ITSM_INBOUND_TOKEN"))
}

func inboundITSMProviderDefault() string {
	if v := strings.TrimSpace(config.EnvOrFile("NMS_ITSM_INBOUND_PROVIDER")); v != "" {
		return v
	}
	if v := strings.TrimSpace(config.EnvOrFile("NMS_ITSM_PROVIDER")); v != "" {
		return v
	}
	return "generic"
}

func applyITSMInboundMapping(status, assignee string, mapping *domain.ITSMInboundMapping) (string, string) {
	if mapping == nil {
		return status, assignee
	}
	effectiveStatus := status
	effectiveAssignee := assignee
	if effectiveStatus == "" {
		effectiveStatus = normalizeInboundIncidentStatus(mapping.MappedStatus)
	}
	if strings.TrimSpace(effectiveAssignee) == "" {
		effectiveAssignee = strings.TrimSpace(mapping.MappedAssignee)
	}
	return effectiveStatus, effectiveAssignee
}

func requestITSMToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-ITSM-Token"))
}

func constantTimeTokenEqual(a, b string) bool {
	if len(a) != len(b) || a == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func decodeAndResolveITSMInbound(w http.ResponseWriter, r *http.Request, resolveMapping func(ctx context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error)) (*itsmInboundResolved, int, string) {
	if r == nil {
		return nil, http.StatusBadRequest, "invalid request"
	}
	want := inboundITSMToken()
	if want == "" {
		return nil, http.StatusServiceUnavailable, "itsm inbound is not configured"
	}
	if !constantTimeTokenEqual(requestITSMToken(r), want) {
		return nil, http.StatusUnauthorized, "unauthorized"
	}
	var input itsmInboundRequest
	if err := decodeJSONBody(w, r, &input); err != nil {
		return nil, http.StatusBadRequest, "invalid json body"
	}
	if input.IncidentID <= 0 {
		return nil, http.StatusBadRequest, "incident_id is required"
	}
	status := normalizeInboundIncidentStatus(input.Status)
	assignee := strings.TrimSpace(input.Assignee)
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = inboundITSMProviderDefault()
	}
	var appliedMappingID int64
	mapping, err := resolveMapping(r.Context(), provider, input.Status, input.Priority, input.Owner)
	if err != nil {
		return nil, http.StatusInternalServerError, err.Error()
	}
	status, assignee = applyITSMInboundMapping(status, assignee, mapping)
	if mapping != nil {
		appliedMappingID = mapping.ID
	}
	if status == "" && assignee == "" {
		return nil, http.StatusBadRequest, "at least one of status or assignee is required (directly or via mapping)"
	}
	return &itsmInboundResolved{
		Input:            input,
		Provider:         provider,
		Status:           status,
		Assignee:         assignee,
		AppliedMappingID: appliedMappingID,
	}, http.StatusOK, ""
}

func (h *Handlers) ITSMInboundWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resolved, code, msg := decodeAndResolveITSMInbound(w, r, h.repo.ResolveITSMInboundMapping)
	if code != http.StatusOK {
		if code >= 500 {
			h.logger.Error("itsm inbound: resolve failed", zap.Int("status", code), zap.String("error", msg))
		}
		http.Error(w, msg, code)
		return
	}
	input := resolved.Input
	changedBy := strings.TrimSpace(input.ChangedBy)
	if changedBy == "" {
		changedBy = "itsm-inbound"
	}
	comment := strings.TrimSpace(input.Comment)

	item, statusChanged, assigneeChanged, err := h.repo.ApplyITSMInboundUpdate(
		r.Context(),
		input.IncidentID,
		resolved.Status,
		resolved.Assignee,
		changedBy,
		comment,
	)
	if err != nil {
		h.logger.Warn("itsm inbound: apply failed",
			zap.Int64("incident_id", input.IncidentID),
			zap.String("status", resolved.Status),
			zap.String("assignee", resolved.Assignee),
			zap.Error(err))
		if strings.Contains(err.Error(), "invalid status transition") {
			http.Error(w, "invalid incident status transition", http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid incident assignment change", http.StatusBadRequest)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	if statusChanged {
		notifyITSMIncidentAsync(h.logger, "incident.status_changed", changedBy, comment, item)
	}
	if assigneeChanged {
		notifyITSMIncidentAsync(h.logger, "incident.assignment_changed", changedBy, comment, item)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":             "ok",
		"incident":           item,
		"applied_mapping_id": resolved.AppliedMappingID,
	})
}

func (h *Handlers) ITSMInboundDryRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resolved, code, msg := decodeAndResolveITSMInbound(w, r, h.repo.ResolveITSMInboundMapping)
	if code != http.StatusOK {
		if code >= 500 {
			h.logger.Error("itsm inbound dry-run: resolve failed", zap.Int("status", code), zap.String("error", msg))
		}
		http.Error(w, msg, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":             "ok",
		"dry_run":            true,
		"provider":           resolved.Provider,
		"incident_id":        resolved.Input.IncidentID,
		"effective_status":   resolved.Status,
		"effective_assignee": resolved.Assignee,
		"applied_mapping_id": resolved.AppliedMappingID,
	})
}
