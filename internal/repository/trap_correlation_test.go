package repository

import "testing"

func TestTrapSignalClassification(t *testing.T) {
	if trapIncidentTitle("IF-MIB::linkDown") != "Link loss detected" {
		t.Fatalf("expected linkDown to map to link loss title")
	}
	if trapIncidentTitle("BFD-MIB::bfdDown") != "Link loss detected" {
		t.Fatalf("expected bfdDown to map to link loss title")
	}
	if trapIncidentTitle("IF-MIB::linkUp") != "Link recovery detected" {
		t.Fatalf("expected linkUp to map to recovery title")
	}
	if !trapIsRecoverySignal("BFD-MIB::bfdUp") {
		t.Fatalf("expected bfdUp to be recovery signal")
	}
	if trapIncidentSeverity("IF-MIB::linkDown") != "critical" {
		t.Fatalf("expected linkDown severity critical")
	}
}
