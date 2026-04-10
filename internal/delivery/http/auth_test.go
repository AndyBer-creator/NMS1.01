package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
