package http

import (
	"testing"
	"time"
)

func resetAlertWebhookTestState() {
	alertWebhookRateMu.Lock()
	alertWebhookRateState = map[string]webhookRateState{}
	alertWebhookRateMu.Unlock()

	alertWebhookDedupeMu.Lock()
	alertWebhookDedupe = map[string]time.Time{}
	alertWebhookDedupeMu.Unlock()
}

func TestAlertWebhookRateLimitPerMinute_DefaultAndInvalid(t *testing.T) {
	resetAlertWebhookTestState()

	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "")
	if got := alertWebhookRateLimitPerMinute(); got != 60 {
		t.Fatalf("default rate limit: got %d want 60", got)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "0")
	if got := alertWebhookRateLimitPerMinute(); got != 60 {
		t.Fatalf("invalid rate limit 0: got %d want 60", got)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "abc")
	if got := alertWebhookRateLimitPerMinute(); got != 60 {
		t.Fatalf("invalid rate limit abc: got %d want 60", got)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "5")
	if got := alertWebhookRateLimitPerMinute(); got != 5 {
		t.Fatalf("explicit rate limit: got %d want 5", got)
	}
}

func TestAllowAlertWebhookRequest_RespectsLimitAndWindow(t *testing.T) {
	resetAlertWebhookTestState()
	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "2")

	now := time.Unix(1_000_000, 0).UTC()
	ip := "1.2.3.4"

	allowed, retry := allowAlertWebhookRequest(ip, now)
	if !allowed || retry != 0 {
		t.Fatalf("first request should be allowed: allowed=%t retry=%s", allowed, retry)
	}
	allowed, retry = allowAlertWebhookRequest(ip, now.Add(1*time.Second))
	if !allowed || retry != 0 {
		t.Fatalf("second request should be allowed: allowed=%t retry=%s", allowed, retry)
	}
	allowed, retry = allowAlertWebhookRequest(ip, now.Add(2*time.Second))
	if allowed {
		t.Fatalf("third request should be blocked")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry should be within (0, 1m]: %s", retry)
	}

	// New window resets.
	allowed, retry = allowAlertWebhookRequest(ip, now.Add(time.Minute+1*time.Second))
	if !allowed || retry != 0 {
		t.Fatalf("request after window should be allowed: allowed=%t retry=%s", allowed, retry)
	}
}

func TestAlertWebhookIdempotencyTTL_DefaultInvalidAndValid(t *testing.T) {
	resetAlertWebhookTestState()

	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "")
	if got := alertWebhookIdempotencyTTL(); got != 2*time.Minute {
		t.Fatalf("default ttl: got %s want %s", got, 2*time.Minute)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "0s")
	if got := alertWebhookIdempotencyTTL(); got != 2*time.Minute {
		t.Fatalf("invalid ttl 0: got %s want %s", got, 2*time.Minute)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "abc")
	if got := alertWebhookIdempotencyTTL(); got != 2*time.Minute {
		t.Fatalf("invalid ttl abc: got %s want %s", got, 2*time.Minute)
	}

	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "3s")
	if got := alertWebhookIdempotencyTTL(); got != 3*time.Second {
		t.Fatalf("valid ttl: got %s want %s", got, 3*time.Second)
	}
}

func TestAlertFingerprint_Deterministic(t *testing.T) {
	resetAlertWebhookTestState()

	start := time.Unix(123, 0).UTC()
	a := alertmanagerWebhookPayloadAlert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "A", "x": "1"},
		Annotations: map[string]string{"summary": "S"},
		StartsAt:    start,
	}
	b := alertmanagerWebhookPayloadAlert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "A", "x": "1"},
		Annotations: map[string]string{"summary": "S"},
		StartsAt:    start,
	}
	fp1 := alertFingerprint(a)
	fp2 := alertFingerprint(b)
	if fp1 == "" {
		t.Fatalf("fingerprint should not be empty")
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint should be deterministic: %q != %q", fp1, fp2)
	}

	b.Labels["x"] = "2"
	fp3 := alertFingerprint(b)
	if fp3 == fp1 {
		t.Fatalf("fingerprint should change when payload changes")
	}
}

func TestShouldProcessAlertFingerprint_DedupesWithinTTL(t *testing.T) {
	resetAlertWebhookTestState()
	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "2s")

	now := time.Unix(1_000_000, 0).UTC()
	fp := "abc"

	if !shouldProcessAlertFingerprint(fp, now) {
		t.Fatalf("first fingerprint should be processed")
	}
	if shouldProcessAlertFingerprint(fp, now.Add(500*time.Millisecond)) {
		t.Fatalf("second fingerprint within TTL should be suppressed")
	}
	if !shouldProcessAlertFingerprint(fp, now.Add(3*time.Second)) {
		t.Fatalf("fingerprint after TTL should be processed again")
	}
}

func TestDefaultIfEmpty(t *testing.T) {
	if got := defaultIfEmpty("", "x"); got != "x" {
		t.Fatalf("empty should return default: %q", got)
	}
	if got := defaultIfEmpty("   ", "x"); got != "x" {
		t.Fatalf("whitespace should return default: %q", got)
	}
	if got := defaultIfEmpty(" y ", "x"); got != "y" {
		t.Fatalf("non-empty should be trimmed: %q", got)
	}
}

