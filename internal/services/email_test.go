package services

import (
	"strings"
	"testing"
)

func TestSMTPClient_Enabled(t *testing.T) {
	if NewSMTPClient("", "587", "", "", "a@b.c").Enabled() {
		t.Fatal("empty host must disable")
	}
	if NewSMTPClient("smtp.example.com", "", "", "", "a@b.c").Enabled() {
		t.Fatal("empty port must disable")
	}
	if NewSMTPClient("smtp.example.com", "587", "", "", "").Enabled() {
		t.Fatal("empty from must disable")
	}
	if !NewSMTPClient("smtp.example.com", "587", "", "", "a@b.c").Enabled() {
		t.Fatal("minimal config must enable")
	}
}

func TestSMTPClient_Send_validation(t *testing.T) {
	c := NewSMTPClient("", "587", "", "", "from@x")
	if err := c.Send("to@x", "s", "b"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want not configured: %v", err)
	}

	c2 := NewSMTPClient("h", "25", "", "", "from@x")
	if err := c2.Send("", "s", "b"); err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("want empty recipient: %v", err)
	}
	if err := c2.Send("   ", "s", "b"); err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("want whitespace recipient: %v", err)
	}
}

func TestAllowPlainSMTPEnv(t *testing.T) {
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	if allowPlainSMTP() {
		t.Fatal("plaintext SMTP must be disabled by default")
	}
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "true")
	if !allowPlainSMTP() {
		t.Fatal("plaintext SMTP override must be enabled for true")
	}
}
