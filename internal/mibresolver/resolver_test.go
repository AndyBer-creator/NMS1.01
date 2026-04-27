package mibresolver

import (
	"strings"
	"testing"
	"time"
)

func TestIsNumericOID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.3.6.1.2.1.1.1.0", true},
		{".1.3.6.1.2.1.1.1.0", true},
		{"1", true},
		{"", false},
		{"1..2", false},
		{"IF-MIB::ifDescr.1", false},
		{"1.3.6.abc.1", false},
	}
	for _, tc := range cases {
		if got := IsNumericOID(tc.in); got != tc.want {
			t.Fatalf("IsNumericOID(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeNumeric(t *testing.T) {
	if got := NormalizeNumeric(".1.3.6.1"); got != "1.3.6.1" {
		t.Fatalf("NormalizeNumeric: got %q", got)
	}
	if got := NormalizeNumeric("  .2.0  "); got != "2.0" {
		t.Fatalf("NormalizeNumeric trim: got %q", got)
	}
}

func TestValidateSymbol(t *testing.T) {
	if err := ValidateSymbol("IF-MIB::ifDescr.1"); err != nil {
		t.Fatalf("valid symbol: %v", err)
	}
	if err := ValidateSymbol(""); err == nil {
		t.Fatal("empty symbol must fail")
	}
	if err := ValidateSymbol("bad;rm"); err == nil {
		t.Fatal("injection chars must fail")
	}
	s512 := strings.Repeat("x", 512)
	if err := ValidateSymbol(s512); err != nil {
		t.Fatalf("512 chars ok: %v", err)
	}
	if err := ValidateSymbol(s512 + "x"); err == nil {
		t.Fatal("513 chars must fail")
	}
}

func TestPickSNMPValue(t *testing.T) {
	m := map[string]string{
		"1.3.6.1.2.1.1.5.0": "host-a",
	}
	if got := PickSNMPValue(m, "1.3.6.1.2.1.1.5.0"); got != "host-a" {
		t.Fatalf("exact key: got %q", got)
	}
	if got := PickSNMPValue(map[string]string{".1.2.3": "v"}, "1.2.3"); got != "v" {
		t.Fatalf("leading dot key: got %q", got)
	}
	if got := PickSNMPValue(map[string]string{"1.2.3": "v"}, ".1.2.3"); got != "v" {
		t.Fatalf("normalize request OID: got %q", got)
	}
	if got := PickSNMPValue(map[string]string{".9.9.9": "only"}, "1.1.1"); got != "only" {
		t.Fatalf("single-entry fallback: got %q", got)
	}
	if got := PickSNMPValue(map[string]string{"a": "x", "b": "y"}, "1.1.1"); got != "" {
		t.Fatalf("ambiguous multi-key: want empty, got %q", got)
	}
}

func TestResolverNegativeCacheExpires(t *testing.T) {
	r := &Resolver{
		cache:            make(map[string]resolveCacheEntry),
		negativeCacheTTL: 20 * time.Millisecond,
	}
	r.storeNegativeResolution("IF-MIB::noSuch", "snmptranslate: unknown object identifier")
	_, errMsg, ok := r.cachedResolution("IF-MIB::noSuch")
	if !ok || errMsg == "" {
		t.Fatalf("expected cached negative entry, got ok=%v errMsg=%q", ok, errMsg)
	}

	time.Sleep(30 * time.Millisecond)
	_, _, ok = r.cachedResolution("IF-MIB::noSuch")
	if ok {
		t.Fatal("expected negative cache entry to expire")
	}
}

func TestResolverPositiveCachePersists(t *testing.T) {
	r := &Resolver{cache: make(map[string]resolveCacheEntry)}
	r.storePositiveResolution("IF-MIB::sysDescr.0", "1.3.6.1.2.1.1.1.0")
	oid, errMsg, ok := r.cachedResolution("IF-MIB::sysDescr.0")
	if !ok || errMsg != "" || oid != "1.3.6.1.2.1.1.1.0" {
		t.Fatalf("unexpected positive cache state: ok=%v oid=%q err=%q", ok, oid, errMsg)
	}
}

func TestIsTranslateNotFoundError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Unknown Object Identifier", true},
		{"Cannot find module (IF-MIB)", true},
		{"some transport error", false},
	}
	for _, tc := range cases {
		if got := isTranslateNotFoundError(tc.msg); got != tc.want {
			t.Fatalf("isTranslateNotFoundError(%q)=%v want=%v", tc.msg, got, tc.want)
		}
	}
}

func TestResolveToNumeric_UsesNegativeCache(t *testing.T) {
	r := &Resolver{cache: make(map[string]resolveCacheEntry), negativeCacheTTL: time.Minute}
	r.storeNegativeResolution("IF-MIB::missing", "snmptranslate: unknown object identifier")
	_, err := r.ResolveToNumeric("IF-MIB::missing")
	if err == nil || !strings.Contains(err.Error(), "unknown object identifier") {
		t.Fatalf("expected cached negative error, got %v", err)
	}
}
