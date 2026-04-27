package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"
	"NMS1/internal/repository"
	"NMS1/internal/testdb"
	"NMS1/internal/usecases/discovery"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	if err := chdirModuleRoot(); err != nil {
		fmt.Fprintf(os.Stderr, "http tests: chdir module root: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func chdirModuleRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if st, e := os.Stat(filepath.Join(dir, "go.mod")); e == nil && !st.IsDir() {
			return os.Chdir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("go.mod not found from starting dir %q", dir)
		}
		dir = parent
	}
}

func httpIntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		t.Skip("integration: set DB_DSN for HTTP tests")
	}
	return dsn
}

func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"NMS_ADMIN_USER", "NMS_ADMIN_PASS",
		"NMS_VIEWER_USER", "NMS_VIEWER_PASS",
	} {
		t.Setenv(k, "")
		t.Setenv(k+"_FILE", "")
	}
}

func clearAlertDeliveryEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"NMS_ALERT_WEBHOOK_TOKEN", "NMS_ALERT_WEBHOOK_TOKEN_FILE",
		"TELEGRAM_TOKEN", "TELEGRAM_CHAT_ID",
		"TELEGRAM_TOKEN_FILE", "TELEGRAM_CHAT_ID_FILE",
		"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM",
		"SMTP_HOST_FILE", "SMTP_PORT_FILE", "SMTP_USER_FILE", "SMTP_PASS_FILE", "SMTP_FROM_FILE",
	} {
		t.Setenv(k, "")
	}
	alertWebhookRateMu.Lock()
	alertWebhookRateState = map[string]webhookRateState{}
	alertWebhookRateMu.Unlock()
	alertWebhookDedupeMu.Lock()
	alertWebhookDedupe = map[string]time.Time{}
	alertWebhookDedupeMu.Unlock()
}

func setAlertWebhookToken(t *testing.T) string {
	t.Helper()
	token := "itest-alert-webhook-token"
	t.Setenv("NMS_ALERT_WEBHOOK_TOKEN", token)
	t.Setenv("NMS_ALERT_WEBHOOK_TOKEN_FILE", "")
	return token
}

// integrationAuthOpts задаёт Basic-учётки для loadCreds(); пустые строки — креды не выставляются (после clearAuth).
type integrationAuthOpts struct {
	AdminUser, AdminPass   string
	ViewerUser, ViewerPass string
	// MibUploadDir если задан — каталог загрузки MIB (иначе t.TempDir()).
	MibUploadDir string
}

func applyIntegrationAuthEnv(t *testing.T, opts integrationAuthOpts) {
	t.Helper()
	clearAuthEnv(t)
	t.Setenv("NMS_ENFORCE_HTTPS", "")
	if opts.AdminUser != "" {
		t.Setenv("NMS_ADMIN_USER", opts.AdminUser)
		t.Setenv("NMS_ADMIN_PASS", opts.AdminPass)
		t.Setenv("NMS_ADMIN_USER_FILE", "")
		t.Setenv("NMS_ADMIN_PASS_FILE", "")
	}
	if opts.ViewerUser != "" {
		t.Setenv("NMS_VIEWER_USER", opts.ViewerUser)
		t.Setenv("NMS_VIEWER_PASS", opts.ViewerPass)
		t.Setenv("NMS_VIEWER_USER_FILE", "")
		t.Setenv("NMS_VIEWER_PASS_FILE", "")
	}
}

func buildIntegrationHandler(t *testing.T, opts integrationAuthOpts) (http.Handler, *postgres.Repo) {
	t.Helper()
	dsn := httpIntegrationDSN(t)
	applyIntegrationAuthEnv(t, opts)

	repo, err := postgres.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	testdb.PingDBOrSkip(t, db, 5*time.Second)

	trapsRepo := repository.NewTrapsRepo(db)
	snmpClient := snmp.New(161, 2*time.Second, 1)
	scanner := discovery.NewScanner(snmpClient, repo, zap.NewNop())
	uploadDir := opts.MibUploadDir
	if uploadDir == "" {
		uploadDir = t.TempDir()
	} else {
		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			t.Fatalf("MkdirAll mib upload dir: %v", err)
		}
	}
	cfg := &config.Config{}
	cfg.Paths.MibUploadDir = uploadDir
	mib := mibresolver.New(config.MIBSearchDirs(cfg), zap.NewNop())
	h := NewHandlers(repo, snmpClient, scanner, trapsRepo, zap.NewNop(), uploadDir, mib)
	return Router(h), repo
}

func newIntegrationServer(t *testing.T, opts integrationAuthOpts) (*httptest.Server, *postgres.Repo) {
	t.Helper()
	h, repo := buildIntegrationHandler(t, opts)
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close() })
	return srv, repo
}

func newIntegrationHTTPClient(t *testing.T) (*http.Client, http.CookieJar) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}, jar
}

// viewerIntegrationCSRF — viewer делает GET /devices (JSON), в jar появляется CSRF cookie; возвращает клиент и токен для POST/DELETE/...
func viewerIntegrationCSRF(t *testing.T, srv *httptest.Server, viewerUser, viewerPass string) (*http.Client, string) {
	t.Helper()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(viewerUser, viewerPass)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /devices (viewer seed): %v", err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("viewer GET /devices status %d", res.StatusCode)
	}
	return client, csrfFromJar(t, jar, base)
}

// adminIntegrationCSRF — admin делает GET /devices (JSON); возвращает клиент, jar и base для повторного csrfFromJar.
func adminIntegrationCSRF(t *testing.T, srv *httptest.Server, adminUser, adminPass string) (*http.Client, http.CookieJar, *url.URL) {
	t.Helper()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /devices (admin seed): %v", err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin GET /devices status %d", res.StatusCode)
	}
	return client, jar, base
}

func testDeviceIP(t *testing.T) string {
	t.Helper()
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("192.0.2.%d", int(b[0])%230+10)
}

func newIntegrationHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := buildIntegrationHandler(t, integrationAuthOpts{})
	return h
}

func csrfFromJar(t *testing.T, jar http.CookieJar, base *url.URL) string {
	t.Helper()
	for _, c := range jar.Cookies(base) {
		if c.Name == csrfCookieName {
			return c.Value
		}
	}
	t.Fatal("CSRF cookie not found")
	return ""
}

func TestIntegration_HTTP_HealthAndMetrics(t *testing.T) {
	h := newIntegrationHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "OK" {
		t.Fatalf("health: %d %q", res.StatusCode, body)
	}

	res2, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = res2.Body.Close() }()
	b2, _ := io.ReadAll(res2.Body)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("metrics status: %d", res2.StatusCode)
	}
	if !strings.Contains(string(b2), "# HELP") && !strings.Contains(string(b2), "nms_") {
		t.Fatalf("metrics body missing expected prometheus exposition")
	}

	res3, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = res3.Body.Close() }()
	b3, _ := io.ReadAll(res3.Body)
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("ready: %d %s", res3.StatusCode, b3)
	}
	var ready map[string]any
	if err := json.Unmarshal(b3, &ready); err != nil {
		t.Fatalf("ready json: %v", err)
	}
	if ready["status"] != "ready" {
		t.Fatalf("ready status: %v", ready["status"])
	}

	res4, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health for request-id: %v", err)
	}
	defer func() { _ = res4.Body.Close() }()
	_, _ = io.Copy(io.Discard, res4.Body)
	if rid := res4.Header.Get("X-Request-ID"); rid == "" {
		t.Fatal("expected X-Request-ID on /health")
	}
}

func TestIntegration_HTTP_HTTPSPolicyRedirectAndBypass(t *testing.T) {
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	const adminUser, adminPass = "itest-https-admin", "itest-https-secret"
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	// Non-bypass route over plain HTTP must redirect before auth middleware.
	req1, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req1.Host = "nms.local"
	clientNoRedirect := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res1, err := clientNoRedirect.Do(req1)
	if err != nil {
		t.Fatalf("GET /devices over HTTP: %v", err)
	}
	_, _ = io.Copy(io.Discard, res1.Body)
	_ = res1.Body.Close()
	if res1.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("expected 308 redirect for /devices over HTTP, got %d", res1.StatusCode)
	}

	// Bypass endpoints must stay on HTTP for probe/scrape compatibility.
	res2, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	body2, _ := io.ReadAll(res2.Body)
	_ = res2.Body.Close()
	if res2.StatusCode != http.StatusOK || string(body2) != "OK" {
		t.Fatalf("health bypass failed: %d %q", res2.StatusCode, body2)
	}

	res3, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	metricsBody, _ := io.ReadAll(res3.Body)
	_ = res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("metrics bypass failed: status %d", res3.StatusCode)
	}
	if !strings.Contains(string(metricsBody), "# HELP") && !strings.Contains(string(metricsBody), "nms_") {
		t.Fatalf("metrics bypass body missing expected prometheus exposition")
	}

	res4, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready bypass: %v", err)
	}
	rb, _ := io.ReadAll(res4.Body)
	_ = res4.Body.Close()
	if res4.StatusCode != http.StatusOK {
		t.Fatalf("ready bypass: %d %s", res4.StatusCode, rb)
	}

	res5, err := http.Get(srv.URL + "/.well-known/security.txt")
	if err != nil {
		t.Fatalf("GET security.txt bypass: %v", err)
	}
	sb, _ := io.ReadAll(res5.Body)
	_ = res5.Body.Close()
	if res5.StatusCode != http.StatusOK {
		t.Fatalf("security.txt bypass: %d %s", res5.StatusCode, sb)
	}
	if !strings.Contains(string(sb), "Contact:") {
		t.Fatalf("security.txt body unexpected: %q", sb)
	}
}

func TestIntegration_HTTP_HTTPSPolicyForwardedProtoSkipsRedirect(t *testing.T) {
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	const adminUser, adminPass = "itest-https-fwd-admin", "itest-https-fwd-secret"
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	// Reverse proxy terminates TLS and forwards HTTP with X-Forwarded-Proto=https.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /devices with forwarded proto https: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 without redirect behind proxy, got %d: %s", res.StatusCode, body)
	}
}

func TestIntegration_HTTP_AlertWebhookMethodNotAllowed(t *testing.T) {
	clearAlertDeliveryEnv(t)
	_ = setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/alerts/webhook", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", res.StatusCode, body)
	}
}

func TestIntegration_HTTP_AlertWebhookUnauthorizedWithoutToken(t *testing.T) {
	clearAlertDeliveryEnv(t)
	_ = setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(`{"status":"firing","alerts":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", res.StatusCode, body)
	}
}

func TestIntegration_HTTP_AlertWebhookInvalidJSON(t *testing.T) {
	clearAlertDeliveryEnv(t)
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader("not-json{"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.StatusCode, body)
	}
}

func TestIntegration_HTTP_AlertWebhookEmptyAlertsNoContent(t *testing.T) {
	clearAlertDeliveryEnv(t)
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(`{"status":"firing","alerts":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
}

func TestIntegration_HTTP_AlertWebhookFiringReturnsJSON(t *testing.T) {
	clearAlertDeliveryEnv(t)
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	payload := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"ItestAlert"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("status field: got %v", out["status"])
	}
	if v, ok := out["alerts_total"].(float64); !ok || int(v) != 1 {
		t.Fatalf("alerts_total: got %v", out["alerts_total"])
	}
}

func TestIntegration_HTTP_AlertWebhookResolvedOnlySkipsDeliveryCounters(t *testing.T) {
	clearAlertDeliveryEnv(t)
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	payload := `{"status":"resolved","alerts":[{"status":"resolved","labels":{"alertname":"ResolvedOnly"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if v, ok := out["alerts_total"].(float64); !ok || int(v) != 1 {
		t.Fatalf("alerts_total: got %v", out["alerts_total"])
	}
	if v, ok := out["email_sent"].(float64); !ok || int(v) != 0 {
		t.Fatalf("email_sent: got %v", out["email_sent"])
	}
	if v, ok := out["email_skipped"].(float64); !ok || int(v) != 0 {
		t.Fatalf("email_skipped: got %v", out["email_skipped"])
	}
}

func TestIntegration_HTTP_AlertWebhookMixedStatusesCountsOnlyFiringDelivery(t *testing.T) {
	clearAlertDeliveryEnv(t)
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	payload := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"FiringOne"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"},{"status":"resolved","labels":{"alertname":"ResolvedTwo"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /alerts/webhook: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if v, ok := out["alerts_total"].(float64); !ok || int(v) != 2 {
		t.Fatalf("alerts_total: got %v", out["alerts_total"])
	}
	if v, ok := out["email_sent"].(float64); !ok || int(v) != 0 {
		t.Fatalf("email_sent: got %v", out["email_sent"])
	}
	if v, ok := out["email_skipped"].(float64); !ok || int(v) != 1 {
		t.Fatalf("email_skipped: got %v", out["email_skipped"])
	}
}

func TestIntegration_HTTP_AlertWebhookRateLimited(t *testing.T) {
	clearAlertDeliveryEnv(t)
	t.Setenv("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN", "1")
	alertWebhookRateMu.Lock()
	alertWebhookRateState = map[string]webhookRateState{}
	alertWebhookRateMu.Unlock()
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	payload := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"RateLimitOne"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"}]}`
	req1, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+token)
	req1.RemoteAddr = "198.51.100.250:50001"
	res1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("POST /alerts/webhook first: %v", err)
	}
	_, _ = io.Copy(io.Discard, res1.Body)
	_ = res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", res1.StatusCode)
	}

	req2, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.RemoteAddr = "198.51.100.250:50001"
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /alerts/webhook second: %v", err)
	}
	body2, _ := io.ReadAll(res2.Body)
	_ = res2.Body.Close()
	if res2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d: %s", res2.StatusCode, body2)
	}
	if res2.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on rate limit")
	}
}

func TestIntegration_HTTP_AlertWebhookIdempotencySuppressesDuplicatePayload(t *testing.T) {
	clearAlertDeliveryEnv(t)
	t.Setenv("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL", "5m")
	token := setAlertWebhookToken(t)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{})
	payload := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"DupOne"},"annotations":{"summary":"sum","description":"desc"},"startsAt":"2026-01-01T00:00:00Z"}]}`

	req1, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+token)
	res1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("POST /alerts/webhook first: %v", err)
	}
	body1, _ := io.ReadAll(res1.Body)
	_ = res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected first request 200, got %d: %s", res1.StatusCode, body1)
	}
	var out1 map[string]any
	if err := json.Unmarshal(body1, &out1); err != nil {
		t.Fatalf("decode first json: %v", err)
	}
	if v, ok := out1["suppressed"].(float64); !ok || int(v) != 0 {
		t.Fatalf("first suppressed: got %v", out1["suppressed"])
	}

	req2, err := http.NewRequest(http.MethodPost, srv.URL+"/alerts/webhook", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /alerts/webhook second: %v", err)
	}
	body2, _ := io.ReadAll(res2.Body)
	_ = res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("expected second request 200, got %d: %s", res2.StatusCode, body2)
	}
	var out2 map[string]any
	if err := json.Unmarshal(body2, &out2); err != nil {
		t.Fatalf("decode second json: %v", err)
	}
	if v, ok := out2["suppressed"].(float64); !ok || int(v) != 1 {
		t.Fatalf("second suppressed: got %v", out2["suppressed"])
	}
}

func TestIntegration_HTTP_ListDevicesJSON(t *testing.T) {
	h := newIntegrationHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	var devices []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&devices); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if devices == nil {
		t.Fatal("expected JSON array, got null")
	}
}

func TestIntegration_HTTP_ListTrapsJSON(t *testing.T) {
	h := newIntegrationHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/traps")
	if err != nil {
		t.Fatalf("GET /traps: %v", err)
	}
	if res.StatusCode == http.StatusNotFound {
		_ = res.Body.Close()
		res, err = http.Get(srv.URL + "/traps/")
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("traps status %d: %s", res.StatusCode, b)
	}
	var traps []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&traps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if traps == nil {
		t.Fatal("expected JSON array")
	}
}

func TestIntegration_HTTP_CreateDeviceUnauthorizedJSON(t *testing.T) {
	const adminUser, adminPass = "itest-unauth-admin", "itest-unauth-secret"
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	payload := `{"ip":"192.0.2.77","name":"nope","community":"public","snmp_version":"v2c"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/devices", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without Basic auth, got %d: %s", res.StatusCode, b)
	}
	if !strings.Contains(string(b), "unauthorized") {
		t.Fatalf("expected JSON error body, got %q", string(b))
	}
}

func TestIntegration_HTTP_CreateAndDeleteDeviceWithAuth(t *testing.T) {
	const adminUser, adminPass = "itest-http-admin", "itest-http-secret-pass"
	srv, repo := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)

	ip := testDeviceIP(t)

	token := csrfFromJar(t, jar, base)
	payload := fmt.Sprintf(`{"ip":%q,"name":"http-itest","community":"public","snmp_version":"v2c"}`, ip)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/devices", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	body1, _ := io.ReadAll(res1.Body)
	_ = res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("create status %d: %s", res1.StatusCode, body1)
	}
	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body1, &created); err != nil || created.ID <= 0 {
		t.Fatalf("create response JSON: %v body=%s", err, body1)
	}
	t.Cleanup(func() { _ = repo.DeleteByID(context.Background(), created.ID) })

	delURL := fmt.Sprintf("%s/devices/%d", srv.URL, created.ID)
	token2 := csrfFromJar(t, jar, base)
	del, err := http.NewRequest(http.MethodDelete, delURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	del.SetBasicAuth(adminUser, adminPass)
	del.Header.Set("X-CSRF-Token", token2)
	res2, err := client.Do(del)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	_, _ = io.Copy(io.Discard, res2.Body)
	_ = res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("delete status %d", res2.StatusCode)
	}
}

func TestIntegration_HTTP_ViewerPostDeviceForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-viewer-admin", "itest-viewer-admin-secret"
		viewerUser, viewerPass = "itest-viewer-ro", "itest-viewer-ro-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	ip := testDeviceIP(t)
	payload := fmt.Sprintf(`{"ip":%q,"name":"viewer-forbidden","community":"public","snmp_version":"v2c"}`, ip)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/devices", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_AdminPostWrongCSRF(t *testing.T) {
	const adminUser, adminPass = "itest-csrf-admin", "itest-csrf-admin-secret"
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	_ = csrfFromJar(t, jar, base)
	ip := testDeviceIP(t)
	payload := fmt.Sprintf(`{"ip":%q,"name":"csrf-wrong","community":"public","snmp_version":"v2c"}`, ip)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/devices", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", "intentionally-wrong-token")
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 CSRF mismatch, got %d: %s", res1.StatusCode, b)
	}
	if !strings.Contains(string(b), "CSRF") {
		t.Fatalf("expected CSRF hint in body, got %q", string(b))
	}
}

func TestIntegration_HTTP_ViewerPostDiscoveryScanForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-disc-admin", "itest-disc-admin-secret"
		viewerUser, viewerPass = "itest-disc-viewer", "itest-disc-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	payload := `{"cidr":"203.0.113.0/28","community":"public","snmp_version":"v2c","max_hosts":4,"concurrency":1}`
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/discovery/scan", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /discovery/scan: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer on discovery scan, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerPostWorkerPollIntervalForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-wpoll-admin", "itest-wpoll-admin-secret"
		viewerUser, viewerPass = "itest-wpoll-viewer", "itest-wpoll-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/worker-poll-interval", strings.NewReader("interval_sec=120"))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/worker-poll-interval: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer on worker poll settings, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_AdminPostWorkerPollIntervalRoundTrip(t *testing.T) {
	const adminUser, adminPass = "itest-wpoll-ok-admin", "itest-wpoll-ok-secret"
	srv, repo := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	prev := repo.GetWorkerPollIntervalSeconds(context.Background())
	t.Cleanup(func() {
		if err := repo.SetWorkerPollIntervalSeconds(context.Background(), prev); err != nil {
			t.Logf("restore worker poll interval: %v", err)
		}
	})

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	token := csrfFromJar(t, jar, base)
	const wantSec = 333
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/worker-poll-interval",
		strings.NewReader(fmt.Sprintf("interval_sec=%d", wantSec)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/worker-poll-interval: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	body, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res1.StatusCode, body)
	}
	if got := repo.GetWorkerPollIntervalSeconds(context.Background()); got != wantSec {
		t.Fatalf("repo interval: got %d want %d", got, wantSec)
	}
}

func TestIntegration_HTTP_ViewerPostSNMPRuntimeForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-snmp-rt-admin", "itest-snmp-rt-admin-secret"
		viewerUser, viewerPass = "itest-snmp-rt-viewer", "itest-snmp-rt-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/snmp-runtime",
		strings.NewReader("timeout_sec=4&retries=1"))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/snmp-runtime: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer on snmp runtime settings, got %d: %s", res.StatusCode, body)
	}
}

func TestIntegration_HTTP_AdminPostSNMPRuntimeRoundTrip(t *testing.T) {
	const adminUser, adminPass = "itest-snmp-rt-ok-admin", "itest-snmp-rt-ok-secret"
	srv, repo := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	prevTimeout := repo.GetSNMPTimeoutSeconds(context.Background(), postgres.DefaultSNMPTimeoutSeconds)
	prevRetries := repo.GetSNMPRetries(context.Background(), postgres.DefaultSNMPRetries)
	t.Cleanup(func() {
		_ = repo.SetSNMPTimeoutSeconds(context.Background(), prevTimeout)
		_ = repo.SetSNMPRetries(context.Background(), prevRetries)
	})

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	token := csrfFromJar(t, jar, base)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/snmp-runtime",
		strings.NewReader("timeout_sec=4&retries=2"))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/snmp-runtime: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, body)
	}
	if got := repo.GetSNMPTimeoutSeconds(context.Background(), postgres.DefaultSNMPTimeoutSeconds); got != 4 {
		t.Fatalf("repo timeout: got %d want 4", got)
	}
	if got := repo.GetSNMPRetries(context.Background(), postgres.DefaultSNMPRetries); got != 2 {
		t.Fatalf("repo retries: got %d want 2", got)
	}
}

func TestIntegration_HTTP_ViewerPostAlertEmailForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-aemail-admin", "itest-aemail-admin-secret"
		viewerUser, viewerPass = "itest-aemail-viewer", "itest-aemail-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/alert-email",
		strings.NewReader("email=itest-viewer-should-not-save@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/alert-email: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer on alert email, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_AdminPostAlertEmailRoundTrip(t *testing.T) {
	const adminUser, adminPass = "itest-aemail-ok-admin", "itest-aemail-ok-secret"
	srv, repo := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	prev := repo.GetAlertEmailTo(context.Background())
	t.Cleanup(func() {
		if err := repo.SetAlertEmailTo(context.Background(), prev); err != nil {
			t.Logf("restore alert email: %v", err)
		}
	})

	const wantEmail = "itest-alert-roundtrip@example.com"

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	token := csrfFromJar(t, jar, base)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/alert-email",
		strings.NewReader("email="+url.QueryEscape(wantEmail)))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/alert-email: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	body, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res1.StatusCode, body)
	}
	if got := repo.GetAlertEmailTo(context.Background()); got != wantEmail {
		t.Fatalf("repo alert email: got %q want %q", got, wantEmail)
	}
}

func TestIntegration_HTTP_ViewerPostMibDeleteForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-mibdel-admin", "itest-mibdel-admin-secret"
		viewerUser, viewerPass = "itest-mibdel-viewer", "itest-mibdel-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/mibs/delete",
		strings.NewReader("name="+url.QueryEscape("dummy.mib")))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /mibs/delete: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on MIB delete, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerPostMibUploadForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-mibup-admin", "itest-mibup-admin-secret"
		viewerUser, viewerPass = "itest-mibup-viewer", "itest-mibup-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "upload-itest.mib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("-- mib integration stub --\n")); err != nil {
		t.Fatal(err)
	}
	contentType := mw.FormDataContentType()
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	post, err := http.NewRequest(http.MethodPost, srv.URL+"/mibs/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", contentType)
	post.Header.Set("X-CSRF-Token", token)
	res, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /mibs/upload: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on MIB upload, got %d: %s", res.StatusCode, b)
	}
}

func TestIntegration_HTTP_AdminPostMibUploadWritesFile(t *testing.T) {
	const (
		adminUser, adminPass = "itest-mibup-ok-admin", "itest-mibup-ok-secret"
		filename             = "integration-admin-upload.mib"
	)
	uploadDir := t.TempDir()
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser:    adminUser,
		AdminPass:    adminPass,
		MibUploadDir: uploadDir,
	})

	_, jar, baseURL := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	token := csrfFromJar(t, jar, baseURL)
	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	content := "-- mib integration admin upload --\nTEST-MIB-DEFINITION ::= BEGIN\nEND\n"
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	ct := mw.FormDataContentType()
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	post, err := http.NewRequest(http.MethodPost, srv.URL+"/mibs/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", ct)
	post.Header.Set("X-CSRF-Token", token)

	res, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /mibs/upload: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after MIB upload, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "mib=ok") {
		t.Fatalf("unexpected Location %q", loc)
	}

	dest := filepath.Join(uploadDir, filename)
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("file content mismatch: got %q want %q", got, content)
	}
}

func TestIntegration_HTTP_ViewerPostTestAlertForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-talert-admin", "itest-talert-admin-secret"
		viewerUser, viewerPass = "itest-talert-viewer", "itest-talert-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	payload := `{"device_ip":"192.0.2.1","oid":"1.3.6.1.6.3.1.1.5.1"}`
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/test-alert", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /test-alert: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on test-alert, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_AdminPostAlertEmailInvalidReturnsPanelWithError(t *testing.T) {
	const adminUser, adminPass = "itest-aemail-bad-admin", "itest-aemail-bad-secret"
	srv, repo := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
	})

	prev := repo.GetAlertEmailTo(context.Background())
	t.Cleanup(func() {
		if err := repo.SetAlertEmailTo(context.Background(), prev); err != nil {
			t.Logf("restore alert email: %v", err)
		}
	})

	client, jar, base := adminIntegrationCSRF(t, srv, adminUser, adminPass)
	token := csrfFromJar(t, jar, base)
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/settings/alert-email",
		strings.NewReader("email="+url.QueryEscape("not-a-valid-email")))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(adminUser, adminPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST /settings/alert-email: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	body, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with validation panel, got %d: %s", res1.StatusCode, body)
	}
	if !strings.Contains(string(body), "Неверный") {
		t.Fatalf("expected validation message in HTML body, got %q", string(body))
	}
	if got := repo.GetAlertEmailTo(context.Background()); got != prev {
		t.Fatalf("repo alert email should be unchanged on validation error, got %q want %q", got, prev)
	}
}

func TestIntegration_HTTP_ViewerDeleteDeviceForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-deldev-admin", "itest-deldev-admin-secret"
		viewerUser, viewerPass = "itest-deldev-viewer", "itest-deldev-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	delURL := fmt.Sprintf("%s/devices/%d", srv.URL, 999001)
	del, err := http.NewRequest(http.MethodDelete, delURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	del.SetBasicAuth(viewerUser, viewerPass)
	del.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(del)
	if err != nil {
		t.Fatalf("DELETE /devices: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on DELETE device, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerPostSNMPSetForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-snmpset-admin", "itest-snmpset-admin-secret"
		viewerUser, viewerPass = "itest-snmpset-viewer", "itest-snmpset-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	setURL := fmt.Sprintf("%s/devices/%d/snmp/set", srv.URL, 999002)
	payload := `{"oid":"1.3.6.1.2.1.1.5.0","type":"OctetString","value":"\"x\"","validate_only":true}`
	post, err := http.NewRequest(http.MethodPost, setURL, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-CSRF-Token", token)
	res1, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST snmp/set: %v", err)
	}
	defer func() { _ = res1.Body.Close() }()
	b, _ := io.ReadAll(res1.Body)
	if res1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on SNMP set, got %d: %s", res1.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerGetDeviceEditForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-editget-admin", "itest-editget-admin-secret"
		viewerUser, viewerPass = "itest-editget-viewer", "itest-editget-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	editURL := fmt.Sprintf("%s/devices/%d/edit", srv.URL, 999003)
	req, err := http.NewRequest(http.MethodGet, editURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(viewerUser, viewerPass)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET device edit: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on GET device edit, got %d: %s", res.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerGetDeviceTerminalForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-term-admin", "itest-term-admin-secret"
		viewerUser, viewerPass = "itest-term-viewer", "itest-term-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	u := fmt.Sprintf("%s/devices/%d/terminal", srv.URL, 999005)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(viewerUser, viewerPass)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET device terminal: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on GET device terminal, got %d: %s", res.StatusCode, b)
	}
}

func TestIntegration_HTTP_ViewerPostDeviceUpdateForbidden(t *testing.T) {
	const (
		adminUser, adminPass   = "itest-upddev-admin", "itest-upddev-admin-secret"
		viewerUser, viewerPass = "itest-upddev-viewer", "itest-upddev-viewer-secret"
	)
	srv, _ := newIntegrationServer(t, integrationAuthOpts{
		AdminUser: adminUser, AdminPass: adminPass,
		ViewerUser: viewerUser, ViewerPass: viewerPass,
	})

	client, token := viewerIntegrationCSRF(t, srv, viewerUser, viewerPass)
	body := "name=nope&community=public&snmp_version=v2c"
	updURL := fmt.Sprintf("%s/devices/%d", srv.URL, 999004)
	post, err := http.NewRequest(http.MethodPost, updURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	post.SetBasicAuth(viewerUser, viewerPass)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-CSRF-Token", token)
	res, err := client.Do(post)
	if err != nil {
		t.Fatalf("POST device update: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer on POST device update, got %d: %s", res.StatusCode, b)
	}
}
