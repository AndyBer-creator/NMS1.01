package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromTrustedProxy_DefaultLoopback(t *testing.T) {
	t.Setenv(trustedProxyEnv, "")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:3333"
	if !fromTrustedProxy(r) {
		t.Fatal("loopback peer must be trusted by default")
	}
}

func TestFromTrustedProxy_CustomCIDR(t *testing.T) {
	t.Setenv(trustedProxyEnv, "10.0.0.0/8")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.12.5.4:3333"
	if !fromTrustedProxy(r) {
		t.Fatal("peer in configured CIDR must be trusted")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "203.0.113.20:3333"
	if fromTrustedProxy(r2) {
		t.Fatal("peer outside configured CIDR must not be trusted")
	}
}
