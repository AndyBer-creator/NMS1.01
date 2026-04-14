package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEqualConstTime(t *testing.T) {
	if !equalConstTime("same", "same") {
		t.Fatal("equal strings")
	}
	if equalConstTime("a", "b") {
		t.Fatal("different same length")
	}
	if equalConstTime("short", "longer") {
		t.Fatal("different length must be false")
	}
}

func TestBasicMatch(t *testing.T) {
	c := basicCred{user: "admin", pass: "s3cr3t", role: roleAdmin}
	if !basicMatch(c, "admin", "s3cr3t") {
		t.Fatal("expected match")
	}
	if basicMatch(c, "admin", "wrong") {
		t.Fatal("wrong password")
	}
	if basicMatch(c, "Admin", "s3cr3t") {
		t.Fatal("user is case-sensitive via constant-time compare")
	}
	if basicMatch(basicCred{user: "", pass: "x", role: roleAdmin}, "", "x") {
		t.Fatal("empty user must not match")
	}
	if basicMatch(basicCred{user: "u", pass: "", role: roleAdmin}, "u", "") {
		t.Fatal("empty pass must not match")
	}
}

func TestUserContext(t *testing.T) {
	if userFromContext(context.Background()) != nil {
		t.Fatal("no user in empty ctx")
	}
	u := &authUser{username: "u", role: roleViewer}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withUser(r, u)
	if got := userFromContext(r.Context()); got != u {
		t.Fatalf("context user: got %v want %v", got, u)
	}
}

func TestRequireAuth_UnauthorizedHTMXSetsHXRedirect(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "")
	t.Setenv("NMS_VIEWER_PASS", "")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")

	nextCalled := false
	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices?page=2", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if nextCalled {
		t.Fatal("next handler must not be called for unauthorized request")
	}
	if got := rr.Header().Get("HX-Redirect"); !strings.HasPrefix(got, "/login?next=") {
		t.Fatalf("expected HX-Redirect to login, got %q", got)
	}
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "Unauthorized" {
		t.Fatalf("unexpected body %q", string(body))
	}
}

func TestRequireAuth_UnauthorizedJSONReturnsJSON(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "")
	t.Setenv("NMS_VIEWER_PASS", "")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")

	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", string(body))
	}
}

func TestRequireAdmin_BlocksWhenUserMissing(t *testing.T) {
	nextCalled := false
	protected := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin-op", nil)
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if nextCalled {
		t.Fatal("next handler must not be called when user is nil")
	}
}

func TestRequireAdmin_AllowsWhenNoAuthExplicitlyEnabled(t *testing.T) {
	t.Setenv("NMS_ALLOW_NO_AUTH", "true")
	nextCalled := false
	protected := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin-op", nil)
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !nextCalled {
		t.Fatal("expected next handler to be called when no-auth is explicitly enabled")
	}
}

func TestRequireAdmin_BlocksViewerWithForbidden(t *testing.T) {
	protected := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/devices", nil)
	req = withUser(req, &authUser{username: "viewer", role: roleViewer})
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "Forbidden") {
		t.Fatalf("expected forbidden body, got %q", string(body))
	}
}

func TestRequireAuth_AllowsBasicAdminAndSetsContextUser(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "viewer")
	t.Setenv("NMS_VIEWER_PASS", "view-secret")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")

	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r.Context())
		if u == nil {
			t.Fatal("expected user in context")
		}
		if u.username != "admin" || u.role != roleAdmin {
			t.Fatalf("unexpected auth user: %+v", u)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAuth_AllowsBasicViewerAndSetsContextUser(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "viewer")
	t.Setenv("NMS_VIEWER_PASS", "view-secret")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")

	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r.Context())
		if u == nil {
			t.Fatal("expected user in context")
		}
		if u.username != "viewer" || u.role != roleViewer {
			t.Fatalf("unexpected auth user: %+v", u)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.SetBasicAuth("viewer", "view-secret")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAuth_AllowsValidSessionCookie(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "")
	t.Setenv("NMS_VIEWER_PASS", "")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")

	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}

	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r.Context())
		if u == nil {
			t.Fatal("expected user in context from session cookie")
		}
		if u.username != "admin" || u.role != roleAdmin {
			t.Fatalf("unexpected auth user from session: %+v", u)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func resetAuthLimiterForTest(t *testing.T) {
	t.Helper()
	prev := authLoginLimiter
	authLoginLimiter = newLoginLimiter()
	t.Cleanup(func() { authLoginLimiter = prev })
}

func TestRequireAuth_NoConfiguredCredsFailsClosed(t *testing.T) {
	t.Setenv("NMS_ADMIN_USER", "")
	t.Setenv("NMS_ADMIN_PASS", "")
	t.Setenv("NMS_VIEWER_USER", "")
	t.Setenv("NMS_VIEWER_PASS", "")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")

	nextCalled := false
	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
	if nextCalled {
		t.Fatal("next must not be called when auth is not configured")
	}
}

func TestRequireAuth_BasicAuthThrottled(t *testing.T) {
	resetAuthLimiterForTest(t)
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_VIEWER_USER", "")
	t.Setenv("NMS_VIEWER_PASS", "")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")

	ip := "203.0.113.50"
	user := "admin"
	now := time.Now()
	for i := 0; i < loginMaxAttemptsUser; i++ {
		authLoginLimiter.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	nextCalled := false
	protected := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.RemoteAddr = ip + ":12345"
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
	if nextCalled {
		t.Fatal("next handler must not be called when throttled")
	}
	if !strings.Contains(rr.Body.String(), `"error":"too_many_requests"`) {
		t.Fatalf("unexpected throttled response body: %q", rr.Body.String())
	}
}

func TestAdminUserFromRequest_BasicAuthThrottled(t *testing.T) {
	resetAuthLimiterForTest(t)
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")

	ip := "203.0.113.51"
	user := "admin"
	now := time.Now()
	for i := 0; i < loginMaxAttemptsUser; i++ {
		authLoginLimiter.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/terminal", nil)
	req.RemoteAddr = ip + ":9999"
	req.SetBasicAuth("admin", "secret")

	res := adminUserFromRequest(req)
	if res.user != nil {
		t.Fatal("expected no user when throttled")
	}
	if res.retryAfter <= 0 {
		t.Fatal("expected positive retryAfter when throttled")
	}
}

func TestAdminUserFromRequest_BasicAuthSuccess(t *testing.T) {
	resetAuthLimiterForTest(t)
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/ws/terminal", nil)
	req.RemoteAddr = "203.0.113.52:9999"
	req.SetBasicAuth("admin", "secret")

	res := adminUserFromRequest(req)
	if res.user == nil {
		t.Fatal("expected admin user")
	}
	if res.user.role != roleAdmin {
		t.Fatalf("expected role %q, got %q", roleAdmin, res.user.role)
	}
	if res.retryAfter != 0 {
		t.Fatalf("expected zero retryAfter, got %v", res.retryAfter)
	}
}
