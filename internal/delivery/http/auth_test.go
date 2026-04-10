package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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
