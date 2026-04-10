package http

import (
	"bytes"
	"context"
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
	dsn := httpIntegrationDSN(t)
	clearAuthEnv(t)
	t.Setenv("NMS_ENFORCE_HTTPS", "")

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

	pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pcancel()
	if err := db.PingContext(pctx); err != nil {
		t.Skipf("integration: postgres unreachable (%v)", err)
	}

	trapsRepo := repository.NewTrapsRepo(db)
	snmpClient := snmp.New(161, 2*time.Second, 1)
	scanner := discovery.NewScanner(snmpClient, repo, zap.NewNop())

	uploadDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Paths.MibUploadDir = uploadDir
	mib := mibresolver.New(config.MIBSearchDirs(cfg), zap.NewNop())

	h := NewHandlers(repo, snmpClient, scanner, trapsRepo, zap.NewNop(), uploadDir, mib)
	return Router(h)
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
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "OK" {
		t.Fatalf("health: %d %q", res.StatusCode, body)
	}

	res2, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer res2.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		res, err = http.Get(srv.URL + "/traps/")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
	}
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

func TestIntegration_HTTP_CreateAndDeleteDeviceWithAuth(t *testing.T) {
	dsn := httpIntegrationDSN(t)
	clearAuthEnv(t)
	const adminUser, adminPass = "itest-http-admin", "itest-http-secret-pass"
	t.Setenv("NMS_ADMIN_USER", adminUser)
	t.Setenv("NMS_ADMIN_PASS", adminPass)
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "")

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

	pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pcancel()
	if err := db.PingContext(pctx); err != nil {
		t.Skipf("integration: postgres unreachable (%v)", err)
	}

	trapsRepo := repository.NewTrapsRepo(db)
	snmpClient := snmp.New(161, 2*time.Second, 1)
	scanner := discovery.NewScanner(snmpClient, repo, zap.NewNop())
	uploadDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Paths.MibUploadDir = uploadDir
	mib := mibresolver.New(config.MIBSearchDirs(cfg), zap.NewNop())
	h := NewHandlers(repo, snmpClient, scanner, trapsRepo, zap.NewNop(), uploadDir, mib)
	srv := httptest.NewServer(Router(h))
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}

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
	res0.Body.Close()
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
	res1.Body.Close()
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
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("delete status %d", res2.StatusCode)
	}
}
