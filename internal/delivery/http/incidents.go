package http

import (
	"NMS1/internal/config"
	"NMS1/internal/domain"
	"NMS1/internal/services"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

var (
	itsmNotifierOnce sync.Once
	itsmNotifierInst *services.ITSMWebhookNotifier
	itsmDispatchOnce sync.Once
	itsmDispatchInst *itsmDispatchQueue
)

func itsmNotifier() *services.ITSMWebhookNotifier {
	itsmNotifierOnce.Do(func() {
		provider := strings.TrimSpace(config.EnvOrFile("NMS_ITSM_PROVIDER"))
		url := strings.TrimSpace(config.EnvOrFile("NMS_ITSM_WEBHOOK_URL"))
		token := strings.TrimSpace(config.EnvOrFile("NMS_ITSM_WEBHOOK_BEARER_TOKEN"))
		timeout := 5 * time.Second
		if v := strings.TrimSpace(os.Getenv("NMS_ITSM_WEBHOOK_TIMEOUT")); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				timeout = d
			}
		}
		retries := 1
		if v := strings.TrimSpace(os.Getenv("NMS_ITSM_WEBHOOK_RETRIES")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				retries = n
			}
		}
		itsmNotifierInst = &services.ITSMWebhookNotifier{
			Provider:    provider,
			WebhookURL:  url,
			BearerToken: token,
			Timeout:     timeout,
			MaxRetries:  retries,
		}
	})
	return itsmNotifierInst
}

type itsmDispatchTask struct {
	logger *zap.Logger
	event  services.ITSMIncidentEvent
}

type itsmDispatchQueue struct {
	ch chan itsmDispatchTask
	n  *services.ITSMWebhookNotifier
}

func itsmDispatchQueueConfig() (size, workers int) {
	size = 256
	workers = 2
	if v := strings.TrimSpace(os.Getenv("NMS_ITSM_QUEUE_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("NMS_ITSM_QUEUE_WORKERS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workers = n
		}
	}
	return size, workers
}

func itsmDispatcher() *itsmDispatchQueue {
	itsmDispatchOnce.Do(func() {
		n := itsmNotifier()
		if n == nil || !n.Enabled() {
			return
		}
		size, workers := itsmDispatchQueueConfig()
		q := &itsmDispatchQueue{
			ch: make(chan itsmDispatchTask, size),
			n:  n,
		}
		for i := 0; i < workers; i++ {
			go q.worker()
		}
		itsmDispatchInst = q
	})
	return itsmDispatchInst
}

func (q *itsmDispatchQueue) worker() {
	for task := range q.ch {
		ctx, cancel := context.WithTimeout(context.Background(), q.n.Timeout+2*time.Second)
		err := q.n.SendIncidentEvent(ctx, task.event)
		cancel()
		if err != nil && task.logger != nil {
			task.logger.Warn("itsm incident webhook failed",
				zap.String("event_type", task.event.EventType),
				zap.Int64("incident_id", task.event.Incident.ID),
				zap.Error(err))
		}
	}
}

func notifyITSMIncidentAsync(logger *zap.Logger, eventType, changedBy, comment string, item *domain.Incident) {
	n := itsmNotifier()
	if n == nil || !n.Enabled() || item == nil {
		return
	}
	q := itsmDispatcher()
	if q == nil {
		return
	}
	task := itsmDispatchTask{
		logger: logger,
		event: services.ITSMIncidentEvent{
			EventType: eventType,
			ChangedBy: changedBy,
			Comment:   comment,
			At:        time.Now().UTC(),
			Incident:  item,
		},
	}
	select {
	case q.ch <- task:
	default:
		if logger != nil {
			logger.Warn("itsm incident queue full, dropping event",
				zap.String("event_type", eventType),
				zap.Int64("incident_id", item.ID))
		}
	}
}

func (h *Handlers) ListIncidents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	var deviceID *int
	if v := strings.TrimSpace(r.URL.Query().Get("device_id")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			deviceID = &n
		}
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	severity := strings.TrimSpace(r.URL.Query().Get("severity"))
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	var cursorAt *time.Time
	var cursorID *int64
	if cursor != "" {
		at, id, err := decodeIncidentCursor(cursor)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cursorAt = &at
		cursorID = &id
	}
	page, err := h.repo.ListIncidentsPage(r.Context(), limit, deviceID, status, severity, cursorAt, cursorID)
	if err != nil {
		h.logger.Error("ListIncidents failed", zap.Error(err))
		if isIncidentListClientError(err) {
			http.Error(w, "invalid incident query", http.StatusBadRequest)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	includePagination := cursor != "" || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_pagination")), "true")
	if includePagination {
		resp := map[string]any{
			"items": page.Items,
		}
		if page.More && len(page.Items) > 0 {
			last := page.Items[len(page.Items)-1]
			resp["next_cursor"] = encodeIncidentCursor(last.UpdatedAt, last.ID)
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	_ = json.NewEncoder(w).Encode(page.Items)
}

func isIncidentListClientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.HasPrefix(msg, "invalid status") || strings.HasPrefix(msg, "invalid severity")
}

func (h *Handlers) IncidentsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.incidentsPageTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}

func (h *Handlers) GetIncident(w http.ResponseWriter, r *http.Request) {
	id, err := incidentIDFromChi(r)
	if err != nil {
		http.Error(w, "bad incident id", http.StatusBadRequest)
		return
	}
	item, err := h.repo.GetIncidentByID(r.Context(), id)
	if err != nil {
		h.logger.Error("GetIncidentByID failed", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	transitions, err := h.repo.ListIncidentTransitions(r.Context(), id, 100)
	if err != nil {
		h.logger.Error("ListIncidentTransitions failed", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"incident":    item,
		"transitions": transitions,
	})
}

func (h *Handlers) CreateIncident(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DeviceID *int             `json:"device_id"`
		Assignee *string          `json:"assignee"`
		Title    string           `json:"title"`
		Severity string           `json:"severity"`
		Source   string           `json:"source"`
		Details  *json.RawMessage `json:"details"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item := &domain.Incident{
		DeviceID: input.DeviceID,
		Assignee: input.Assignee,
		Title:    input.Title,
		Severity: input.Severity,
		Source:   input.Source,
	}
	if input.Details != nil {
		item.Details = *input.Details
	}
	out, err := h.repo.CreateIncident(r.Context(), item)
	if err != nil {
		h.logger.Error("CreateIncident failed", zap.Error(err))
		http.Error(w, "invalid incident payload", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
	incidentsCreatedTotal.WithLabelValues(out.Source, out.Severity).Inc()
	notifyITSMIncidentAsync(h.logger, "incident.created", "system", "created via API", out)
}

func (h *Handlers) TransitionIncident(w http.ResponseWriter, r *http.Request) {
	id, err := incidentIDFromChi(r)
	if err != nil {
		http.Error(w, "bad incident id", http.StatusBadRequest)
		return
	}
	var input struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	changedBy := "system"
	if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
		changedBy = strings.TrimSpace(u.username)
	}
	before, err := h.repo.GetIncidentByID(r.Context(), id)
	if err != nil {
		h.logger.Error("GetIncidentByID before transition failed", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if before == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	item, err := h.repo.TransitionIncidentStatus(r.Context(), id, input.Status, changedBy, input.Comment)
	if err != nil {
		h.logger.Error("TransitionIncidentStatus failed", zap.Error(err))
		http.Error(w, "invalid incident status transition", http.StatusBadRequest)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
	incidentTransitionsTotal.WithLabelValues(before.Status, item.Status, item.Source, item.Severity).Inc()
	ageSec := item.UpdatedAt.Sub(item.CreatedAt).Seconds()
	if ageSec < 0 {
		ageSec = 0
	}
	if item.Status == "acknowledged" {
		incidentAckLatencySeconds.WithLabelValues(item.Source, item.Severity).Observe(ageSec)
	}
	if item.Status == "resolved" || item.Status == "closed" {
		incidentResolveLatencySeconds.WithLabelValues(item.Source, item.Severity, item.Status).Observe(ageSec)
	}
	notifyITSMIncidentAsync(h.logger, "incident.status_changed", changedBy, input.Comment, item)
}

func (h *Handlers) AssignIncident(w http.ResponseWriter, r *http.Request) {
	id, err := incidentIDFromChi(r)
	if err != nil {
		http.Error(w, "bad incident id", http.StatusBadRequest)
		return
	}
	var input struct {
		Assignee string `json:"assignee"`
		Comment  string `json:"comment"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	changedBy := "system"
	if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
		changedBy = strings.TrimSpace(u.username)
	}
	item, err := h.repo.AssignIncident(r.Context(), id, input.Assignee, changedBy, input.Comment)
	if err != nil {
		h.logger.Error("AssignIncident failed", zap.Error(err))
		http.Error(w, "invalid incident assignment change", http.StatusBadRequest)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
	notifyITSMIncidentAsync(h.logger, "incident.assignment_changed", changedBy, input.Comment, item)
}

func (h *Handlers) BulkTransitionIncidents(w http.ResponseWriter, r *http.Request) {
	type bulkResult struct {
		IncidentID int64            `json:"incident_id"`
		Success    bool             `json:"success"`
		Error      string           `json:"error,omitempty"`
		Incident   *domain.Incident `json:"incident,omitempty"`
	}
	var input struct {
		IncidentIDs []int64 `json:"incident_ids"`
		Status      string  `json:"status"`
		Comment     string  `json:"comment"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if len(input.IncidentIDs) == 0 {
		http.Error(w, "incident_ids is required", http.StatusBadRequest)
		return
	}
	changedBy := "system"
	if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
		changedBy = strings.TrimSpace(u.username)
	}

	results := make([]bulkResult, 0, len(input.IncidentIDs))
	updatedItems := make([]domain.Incident, 0, len(input.IncidentIDs))
	seen := make(map[int64]struct{}, len(input.IncidentIDs))
	failedCount := 0
	for _, id := range input.IncidentIDs {
		if id <= 0 {
			failedCount++
			results = append(results, bulkResult{
				IncidentID: id,
				Success:    false,
				Error:      "incident id must be > 0",
			})
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		item, err := h.repo.TransitionIncidentStatus(r.Context(), id, input.Status, changedBy, input.Comment)
		if err != nil {
			failedCount++
			results = append(results, bulkResult{
				IncidentID: id,
				Success:    false,
				Error:      err.Error(),
			})
			continue
		}
		if item == nil {
			failedCount++
			results = append(results, bulkResult{
				IncidentID: id,
				Success:    false,
				Error:      "incident not found",
			})
			continue
		}
		updatedItems = append(updatedItems, *item)
		notifyITSMIncidentAsync(h.logger, "incident.status_changed", changedBy, input.Comment, item)
		results = append(results, bulkResult{
			IncidentID: id,
			Success:    true,
			Incident:   item,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"requested_count": len(input.IncidentIDs),
		"updated_count":   len(updatedItems),
		"failed_count":    failedCount,
		"items":           updatedItems,
		"results":         results,
	})
}

func incidentIDFromChi(r *http.Request) (int64, error) {
	s := strings.TrimSpace(chi.URLParam(r, "incidentID"))
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}

func encodeIncidentCursor(updatedAt time.Time, id int64) string {
	raw := fmt.Sprintf("%d:%d", updatedAt.UnixNano(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeIncidentCursor(cursor string) (time.Time, int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("bad cursor format")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return time.Time{}, 0, fmt.Errorf("bad cursor id")
	}
	return time.Unix(0, ns).UTC(), id, nil
}
