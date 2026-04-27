package http

import (
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newLoginTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	tmpl := template.Must(template.New("login.html").Parse(`{{define "login.html"}}err={{.Error}};next={{.Next}}{{end}}`))
	return &Handlers{
		loginTmpl: tmpl,
		logger:    zap.NewNop(),
	}
}

func resetLoginLimiterForTest(t *testing.T) {
	t.Helper()
	prev := authLoginLimiter
	authLoginLimiter = newLoginLimiter()
	t.Cleanup(func() { authLoginLimiter = prev })
}

func setAuthCredsForTest(t *testing.T, adminUser, adminPass, viewerUser, viewerPass string) {
	t.Helper()
	t.Setenv("NMS_ADMIN_USER", adminUser)
	t.Setenv("NMS_ADMIN_PASS", adminPass)
	t.Setenv("NMS_VIEWER_USER", viewerUser)
	t.Setenv("NMS_VIEWER_PASS", viewerPass)
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")
	t.Setenv("NMS_VIEWER_USER_FILE", "")
	t.Setenv("NMS_VIEWER_PASS_FILE", "")
}

func TestLoginPage_MethodNotAllowed(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	rr := httptest.NewRecorder()

	h.LoginPage(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestLoginPage_NoCredsRedirectsHome(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "", "", "", "")
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()

	h.LoginPage(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected redirect to /, got %q", loc)
	}
}

func TestLoginPage_WithSessionRedirectsToSafeNext(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	t.Setenv("NMS_SESSION_SECRET", "itest-secret")
	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/login?next=https://evil.example/pwn", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	h.LoginPage(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected sanitized redirect to /, got %q", loc)
	}
}

func TestLoginPage_RendersTemplateWithSafeNext(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	req := httptest.NewRequest(http.MethodGet, "/login?next=/devices%3Ftab%3Dall", nil)
	rr := httptest.NewRecorder()

	h.LoginPage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "next=/devices?tab=all") {
		t.Fatalf("expected rendered safe next, got %q", body)
	}
}

func TestLoginPost_MethodNotAllowed(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestLoginPost_NoCredsRedirectsHome(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "", "", "", "")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=u&password=p"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected redirect to /, got %q", loc)
	}
}

func TestLoginPost_BadFormReturnsBadRequest(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLoginPost_LargeBodyReturnsRequestEntityTooLarge(t *testing.T) {
	h := newLoginTestHandlers(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	t.Setenv("NMS_LOGIN_MAX_BODY_BYTES", "32")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret&padding=abcdefghijklmnopqrstuvwxyz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rr.Code)
	}
}

func TestLoginPost_InvalidCredentialsUnauthorized(t *testing.T) {
	h := newLoginTestHandlers(t)
	resetLoginLimiterForTest(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrong")
	form.Set("next", "/devices")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.10:12345"
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := rr.Header().Get("Set-Cookie"); strings.Contains(got, sessionCookieName+"=") {
		t.Fatalf("did not expect session cookie on failed login, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "Неверный логин или пароль.") {
		t.Fatalf("expected invalid credentials message, got %q", rr.Body.String())
	}
}

func TestLoginPost_SuccessSetsCookieAndRedirects(t *testing.T) {
	h := newLoginTestHandlers(t)
	resetLoginLimiterForTest(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	t.Setenv("NMS_SESSION_SECRET", "itest-secret")

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "secret")
	form.Set("next", "/devices")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.11:12345"
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/devices" {
		t.Fatalf("expected redirect to /devices, got %q", loc)
	}
	foundCookie := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			foundCookie = true
			break
		}
	}
	if !foundCookie {
		t.Fatalf("expected non-empty %s cookie", sessionCookieName)
	}
}

func TestLoginPost_SuccessSanitizesUnsafeNext(t *testing.T) {
	h := newLoginTestHandlers(t)
	resetLoginLimiterForTest(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	t.Setenv("NMS_SESSION_SECRET", "itest-secret")

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "secret")
	form.Set("next", "https://evil.example/pwn")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.12:12345"
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected sanitized redirect to /, got %q", loc)
	}
}

func TestLoginPost_ThrottledReturns429AndRetryAfter(t *testing.T) {
	h := newLoginTestHandlers(t)
	resetLoginLimiterForTest(t)
	setAuthCredsForTest(t, "admin", "secret", "", "")
	ip := "203.0.113.50"
	user := "admin"
	now := time.Now()
	for i := 0; i < loginMaxAttemptsUser; i++ {
		authLoginLimiter.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	form := url.Values{}
	form.Set("username", user)
	form.Set("password", "secret")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":34567"
	rr := httptest.NewRecorder()

	h.LoginPost(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header")
	}
	b, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(b), "Слишком много попыток входа") {
		t.Fatalf("expected throttle message, got %q", string(b))
	}
}

func TestLogout_MethodNotAllowed(t *testing.T) {
	h := newLoginTestHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rr := httptest.NewRecorder()

	h.Logout(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestLogout_ClearsSessionCookieAndRedirects(t *testing.T) {
	h := newLoginTestHandlers(t)
	t.Setenv("NMS_SESSION_SECRET", "itest-secret")
	resetSessionRevocationsForTest(t)
	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	h.Logout(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected redirect to /login, got %q", loc)
	}

	foundCleared := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			if c.MaxAge >= 0 {
				t.Fatalf("expected cookie MaxAge<0 for clearing, got %d", c.MaxAge)
			}
			if c.Value != "" {
				t.Fatalf("expected cleared cookie value, got %q", c.Value)
			}
			foundCleared = true
			break
		}
	}
	if !foundCleared {
		t.Fatalf("expected %s clearing cookie", sessionCookieName)
	}
	if got := verifySessionToken(token); got != nil {
		t.Fatalf("expected token revoked on logout, got %+v", got)
	}
}
