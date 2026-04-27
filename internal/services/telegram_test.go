package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTelegramAlert_SendCriticalTrap_OK(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	ta := NewTelegramAlert("test-token", "12345")
	ta.HTTPClient = &http.Client{Transport: &rewriteHostTransport{
		base:   http.DefaultTransport,
		target: ts.URL,
	}}

	err := ta.SendCriticalTrap("10.0.0.1", "1.3.6.1.4.1", "var-data")
	if err != nil {
		t.Fatalf("SendCriticalTrap: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method: %s", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/bottest-token/sendMessage") {
		t.Fatalf("path: %s", gotPath)
	}
	var payload map[string]string
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload["chat_id"] != "12345" {
		t.Fatalf("chat_id: %q", payload["chat_id"])
	}
	if !strings.Contains(payload["text"], "10.0.0.1") || !strings.Contains(payload["text"], "1.3.6.1.4.1") {
		t.Fatalf("text: %q", payload["text"])
	}
}

func TestTelegramAlert_SendCriticalTrap_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer ts.Close()

	ta := NewTelegramAlert("t", "1")
	ta.HTTPClient = &http.Client{Transport: &rewriteHostTransport{
		base:   http.DefaultTransport,
		target: ts.URL,
	}}

	err := ta.SendCriticalTrap("1.1.1.1", "o", "v")
	if err == nil || !strings.Contains(err.Error(), "telegram API error") {
		t.Fatalf("want API error, got %v", err)
	}
}

func TestTelegramAlert_SendCriticalTrap_TruncatesVarsByRunes(t *testing.T) {
	var payload map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	ta := NewTelegramAlert("t", "1")
	ta.HTTPClient = &http.Client{Transport: &rewriteHostTransport{
		base:   http.DefaultTransport,
		target: ts.URL,
	}}

	// 150 runes, all non-ASCII, to verify rune-safe truncation.
	vars := strings.Repeat("Ж", 150)
	if err := ta.SendCriticalTrap("10.0.0.1", "1.3.6", vars); err != nil {
		t.Fatalf("SendCriticalTrap: %v", err)
	}
	if !utf8.ValidString(payload["text"]) {
		t.Fatalf("telegram text must stay valid utf-8: %q", payload["text"])
	}
	if strings.Count(payload["text"], "Ж") < 100 {
		t.Fatalf("expected at least 100 rune chars preserved in text: %q", payload["text"])
	}
	if strings.Contains(payload["text"], strings.Repeat("Ж", 130)) {
		t.Fatalf("vars were not truncated to expected size: %q", payload["text"])
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 3); got != "abc" {
		t.Fatalf("ascii truncate mismatch: %q", got)
	}
	if got := truncateRunes("Привет", 4); got != "Прив" {
		t.Fatalf("unicode truncate mismatch: %q", got)
	}
	if got := truncateRunes("ok", 0); got != "" {
		t.Fatalf("zero max should return empty string: %q", got)
	}
}

// rewriteHostTransport перенаправляет запросы к api.telegram.org на httptest.URL.
type rewriteHostTransport struct {
	base   http.RoundTripper
	target string
}

func (rt *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.base == nil {
		rt.base = http.DefaultTransport
	}
	tu, err := url.Parse(rt.target)
	if err != nil {
		return nil, err
	}
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = tu.Scheme
	req2.URL.Host = tu.Host
	return rt.base.RoundTrip(req2)
}
