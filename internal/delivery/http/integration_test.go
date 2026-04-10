package http

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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

// integrationAuthOpts задаёт Basic-учётки для loadCreds(); пустые строки — креды не выставляются (после clearAuth).
type integrationAuthOpts struct {
	AdminUser, AdminPass   string
	ViewerUser, ViewerPass string
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
	uploadDir := t.TempDir()
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

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)

	ip := testDeviceIP(t)
	t.Cleanup(func() { _ = repo.DeleteByIP(ip) })

	getDevices, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	getDevices.SetBasicAuth(adminUser, adminPass)
	getDevices.Header.Set("Accept", "application/json")
	res0, err := client.Do(getDevices)
	if err != nil {
		t.Fatalf("seed GET /devices: %v", err)
	}
	_, _ = io.Copy(io.Discard, res0.Body)
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusOK {
		t.Fatalf("seed status %d", res0.StatusCode)
	}

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

	delURL := fmt.Sprintf("%s/devices/%s", srv.URL, url.PathEscape(ip))
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

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)

	getDevices, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	getDevices.SetBasicAuth(adminUser, adminPass)
	getDevices.Header.Set("Accept", "application/json")
	res0, err := client.Do(getDevices)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	_, _ = io.Copy(io.Discard, res0.Body)
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusOK {
		t.Fatalf("seed status %d", res0.StatusCode)
	}

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

	prev := repo.GetWorkerPollIntervalSeconds()
	t.Cleanup(func() {
		if err := repo.SetWorkerPollIntervalSeconds(prev); err != nil {
			t.Logf("restore worker poll interval: %v", err)
		}
	})

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)

	getDevices, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	getDevices.SetBasicAuth(adminUser, adminPass)
	getDevices.Header.Set("Accept", "application/json")
	res0, err := client.Do(getDevices)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	_, _ = io.Copy(io.Discard, res0.Body)
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusOK {
		t.Fatalf("seed status %d", res0.StatusCode)
	}

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
	if got := repo.GetWorkerPollIntervalSeconds(); got != wantSec {
		t.Fatalf("repo interval: got %d want %d", got, wantSec)
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

	prev := repo.GetAlertEmailTo()
	t.Cleanup(func() {
		if err := repo.SetAlertEmailTo(prev); err != nil {
			t.Logf("restore alert email: %v", err)
		}
	})

	const wantEmail = "itest-alert-roundtrip@example.com"

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)

	getDevices, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	getDevices.SetBasicAuth(adminUser, adminPass)
	getDevices.Header.Set("Accept", "application/json")
	res0, err := client.Do(getDevices)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	_, _ = io.Copy(io.Discard, res0.Body)
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusOK {
		t.Fatalf("seed status %d", res0.StatusCode)
	}

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
	if got := repo.GetAlertEmailTo(); got != wantEmail {
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

	prev := repo.GetAlertEmailTo()
	t.Cleanup(func() {
		if err := repo.SetAlertEmailTo(prev); err != nil {
			t.Logf("restore alert email: %v", err)
		}
	})

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, jar := newIntegrationHTTPClient(t)

	getDevices, err := http.NewRequest(http.MethodGet, srv.URL+"/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	getDevices.SetBasicAuth(adminUser, adminPass)
	getDevices.Header.Set("Accept", "application/json")
	res0, err := client.Do(getDevices)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	_, _ = io.Copy(io.Discard, res0.Body)
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusOK {
		t.Fatalf("seed status %d", res0.StatusCode)
	}

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
	if got := repo.GetAlertEmailTo(); got != prev {
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
	targetIP := "192.0.2.88"
	delURL := fmt.Sprintf("%s/devices/%s", srv.URL, url.PathEscape(targetIP))
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
	targetIP := "192.0.2.89"
	setURL := fmt.Sprintf("%s/devices/%s/snmp/set", srv.URL, url.PathEscape(targetIP))
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

	targetIP := "192.0.2.90"
	editURL := fmt.Sprintf("%s/devices/%s/edit", srv.URL, url.PathEscape(targetIP))
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
	targetIP := "192.0.2.91"
	body := "name=nope&community=public&snmp_version=v2c"
	updURL := fmt.Sprintf("%s/devices/%s", srv.URL, url.PathEscape(targetIP))
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
