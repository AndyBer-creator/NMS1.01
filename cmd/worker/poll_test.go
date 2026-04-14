package main

import "testing"

func TestPollWorkerConcurrency_DefaultAndBounds(t *testing.T) {
	t.Setenv("NMS_WORKER_POLL_CONCURRENCY", "")
	if got := pollWorkerConcurrency(); got != 4 {
		t.Fatalf("default concurrency: got %d want %d", got, 4)
	}

	t.Setenv("NMS_WORKER_POLL_CONCURRENCY", "16")
	if got := pollWorkerConcurrency(); got != 16 {
		t.Fatalf("explicit concurrency: got %d want %d", got, 16)
	}

	t.Setenv("NMS_WORKER_POLL_CONCURRENCY", "0")
	if got := pollWorkerConcurrency(); got != 4 {
		t.Fatalf("invalid low concurrency should fallback: got %d", got)
	}

	t.Setenv("NMS_WORKER_POLL_CONCURRENCY", "999")
	if got := pollWorkerConcurrency(); got != 128 {
		t.Fatalf("high concurrency must clamp: got %d want %d", got, 128)
	}
}

func TestPollRateLimitPerSec_DefaultAndBounds(t *testing.T) {
	t.Setenv("NMS_WORKER_POLL_RATE_LIMIT_PER_SEC", "")
	if got := pollRateLimitPerSec(); got != 0 {
		t.Fatalf("default rate limit: got %d want %d", got, 0)
	}

	t.Setenv("NMS_WORKER_POLL_RATE_LIMIT_PER_SEC", "25")
	if got := pollRateLimitPerSec(); got != 25 {
		t.Fatalf("explicit rate limit: got %d want %d", got, 25)
	}

	t.Setenv("NMS_WORKER_POLL_RATE_LIMIT_PER_SEC", "-1")
	if got := pollRateLimitPerSec(); got != 0 {
		t.Fatalf("invalid negative rate should disable: got %d", got)
	}

	t.Setenv("NMS_WORKER_POLL_RATE_LIMIT_PER_SEC", "5000")
	if got := pollRateLimitPerSec(); got != 1000 {
		t.Fatalf("high rate must clamp: got %d want %d", got, 1000)
	}
}
