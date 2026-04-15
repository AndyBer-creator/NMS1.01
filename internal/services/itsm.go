package services

import (
	"NMS1/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ITSMWebhookNotifier struct {
	Provider    string
	WebhookURL  string
	BearerToken string
	Timeout     time.Duration
	MaxRetries  int
	HTTPClient  *http.Client
}

type ITSMIncidentEvent struct {
	EventType string           `json:"event_type"`
	ChangedBy string           `json:"changed_by,omitempty"`
	Comment   string           `json:"comment,omitempty"`
	At        time.Time        `json:"at"`
	Incident  *domain.Incident `json:"incident"`
}

func (n *ITSMWebhookNotifier) Enabled() bool {
	return strings.TrimSpace(n.WebhookURL) != ""
}

func (n *ITSMWebhookNotifier) SendIncidentEvent(ctx context.Context, event ITSMIncidentEvent) error {
	if !n.Enabled() {
		return nil
	}
	if event.Incident == nil {
		return fmt.Errorf("incident payload is required")
	}
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	retries := n.MaxRetries
	if retries < 0 {
		retries = 0
	}
	payloadBody, err := n.payloadForProvider(event)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(payloadBody)
	if err != nil {
		return fmt.Errorf("marshal itsm event: %w", err)
	}
	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.WebhookURL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build itsm request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if strings.TrimSpace(n.BearerToken) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(n.BearerToken))
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("itsm webhook returned status %d", resp.StatusCode)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return lastErr
			}
		}
		if attempt < retries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
			}
		}
	}
	return lastErr
}

func (n *ITSMWebhookNotifier) payloadForProvider(event ITSMIncidentEvent) (any, error) {
	provider := strings.ToLower(strings.TrimSpace(n.Provider))
	switch provider {
	case "", "generic", "webhook":
		return event, nil
	case "jira":
		return mapJiraPayload(event), nil
	case "servicenow", "service-now", "snow":
		return mapServiceNowPayload(event), nil
	default:
		return nil, fmt.Errorf("unsupported itsm provider %q", n.Provider)
	}
}

func mapJiraPayload(event ITSMIncidentEvent) map[string]any {
	inc := event.Incident
	priority := mapJiraPriority(inc.Severity)
	status := strings.ReplaceAll(inc.Status, "_", " ")
	description := fmt.Sprintf(
		"NMS incident event: %s\nIncident ID: %d\nStatus: %s\nSeverity: %s\nSource: %s\nChangedBy: %s\nComment: %s",
		event.EventType,
		inc.ID,
		inc.Status,
		inc.Severity,
		inc.Source,
		event.ChangedBy,
		event.Comment,
	)
	return map[string]any{
		"fields": map[string]any{
			"summary":     fmt.Sprintf("[NMS] %s", inc.Title),
			"description": description,
			"labels":      []string{"nms", "incident", strings.ToLower(inc.Source)},
			"priority": map[string]any{
				"name": priority,
			},
			"issuetype": map[string]any{
				"name": "Incident",
			},
			"status": map[string]any{
				"name": status,
			},
		},
		"nms_meta": map[string]any{
			"event_type": event.EventType,
			"incident":   inc,
		},
	}
}

func mapServiceNowPayload(event ITSMIncidentEvent) map[string]any {
	inc := event.Incident
	return map[string]any{
		"short_description": fmt.Sprintf("[NMS] %s", inc.Title),
		"description": fmt.Sprintf(
			"NMS incident event=%s id=%d source=%s severity=%s status=%s changed_by=%s comment=%s",
			event.EventType,
			inc.ID,
			inc.Source,
			inc.Severity,
			inc.Status,
			event.ChangedBy,
			event.Comment,
		),
		"severity":  mapServiceNowSeverity(inc.Severity),
		"state":     mapServiceNowState(inc.Status),
		"category":  "network",
		"subcategory": "snmp",
		"u_nms_incident_id": fmt.Sprintf("%d", inc.ID),
		"u_nms_source":      inc.Source,
		"u_nms_event_type":  event.EventType,
	}
}

func mapJiraPriority(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "Highest"
	case "info":
		return "Low"
	default:
		return "Medium"
	}
}

func mapServiceNowSeverity(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 1
	case "info":
		return 3
	default:
		return 2
	}
}

func mapServiceNowState(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "new":
		return 1
	case "acknowledged", "in_progress":
		return 2
	case "resolved":
		return 6
	case "closed":
		return 7
	default:
		return 1
	}
}
