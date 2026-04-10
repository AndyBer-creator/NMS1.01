package lldp

import "testing"

func TestNormalizeKey(t *testing.T) {
	if got := normalizeKey("  Switch-A  "); got != "switch-a" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeOID(t *testing.T) {
	if got := normalizeOID("  .1.2.3.4 "); got != "1.2.3.4" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSingleIndexFromWalk(t *testing.T) {
	n, ok := parseSingleIndexFromWalk(lldpLocPortDescBase+".7", lldpLocPortDescBase)
	if !ok || n != 7 {
		t.Fatalf("got %d ok=%v", n, ok)
	}
	if _, ok := parseSingleIndexFromWalk("1.2.3", lldpLocPortDescBase); ok {
		t.Fatal("unrelated OID must fail")
	}
}

func TestParseRemoteIndexes(t *testing.T) {
	local, rem, ok := parseRemoteIndexes(lldpRemSysNameBase+".0.5.2", lldpRemSysNameBase)
	if !ok || local != 5 || rem != 2 {
		t.Fatalf("got local=%d rem=%d ok=%v", local, rem, ok)
	}
	if _, _, ok := parseRemoteIndexes(lldpRemSysNameBase+".0.5", lldpRemSysNameBase); ok {
		t.Fatal("short suffix must fail")
	}
}
