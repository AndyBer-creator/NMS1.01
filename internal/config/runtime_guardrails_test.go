package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	if strings.HasPrefix(strings.TrimSpace(content), "example-host ssh-") {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		pub, err := ssh.NewPublicKey(priv.Public())
		if err != nil {
			t.Fatalf("NewPublicKey: %v", err)
		}
		content = knownhosts.Line([]string{"example-host"}, pub) + "\n"
	}
	f, err := os.CreateTemp(t.TempDir(), "nms-guardrails-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return f.Name()
}

func TestValidateRuntimeSecurity_NonProductionNoop(t *testing.T) {
	t.Setenv("NMS_ENV", "docker")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "")
	t.Setenv("NMS_REQUIRE_PROD_GUARDRAILS", "")
	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected no error for non-production, got %v", err)
	}
}

func TestValidateRuntimeSecurity_NonProductionCanRequireProdGuardrails(t *testing.T) {
	t.Setenv("NMS_ENV", "docker")
	t.Setenv("NMS_REQUIRE_PROD_GUARDRAILS", "true")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "")
	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected guardrail error when NMS_REQUIRE_PROD_GUARDRAILS=true")
	}
}

func TestValidateRuntimeSecurity_NonProductionWithForcedGuardrailsCanPass(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForced")
	t.Setenv("NMS_ENV", "docker")
	t.Setenv("NMS_REQUIRE_PROD_GUARDRAILS", "true")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected forced guardrails config to pass, got %v", err)
	}
}

func TestValidateRuntimeSecurity_ProductionRequiresSecretsAndSafeFlags(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected valid production config, got %v", err)
	}
}

func TestValidateRuntimeSecurityFor_WorkerDoesNotRequireAPISpecificSettings(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")

	if err := ValidateRuntimeSecurityFor(RuntimeSecurityRoleWorker); err != nil {
		t.Fatalf("expected worker config to pass without API-only settings, got %v", err)
	}
}

func TestValidateRuntimeSecurityFor_TrapReceiverDoesNotRequireAPISpecificSettings(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")

	if err := ValidateRuntimeSecurityFor(RuntimeSecurityRoleTrapReceiver); err != nil {
		t.Fatalf("expected trap receiver config to pass without API-only settings, got %v", err)
	}
}

func TestValidateRuntimeSecurityFor_WorkerStillRejectsInsecureSMTPOverride(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "true")

	if err := ValidateRuntimeSecurityFor(RuntimeSecurityRoleWorker); err == nil {
		t.Fatal("expected worker to reject NMS_SMTP_ALLOW_PLAINTEXT in production")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsInsecureSettings(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=disable")
	t.Setenv("NMS_ALLOW_NO_AUTH", "true")
	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for insecure production settings")
	}
}

func TestValidateRuntimeSecurity_ProductionRequiresKnownHosts(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error when NMS_TERMINAL_SSH_KNOWN_HOSTS is missing")
	}
}

func TestValidateRuntimeSecurity_ProductionRequiresHTTPSPolicy(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQFake")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "")
	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error when NMS_ENFORCE_HTTPS is not enabled")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsMissingDBSSLMode(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey2")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error when DB_DSN has no safe sslmode")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsInvalidDBSSLModeValue(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=requireX")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for invalid DB_DSN sslmode value")
	}
}

func TestValidateRuntimeSecurity_ProductionAcceptsURLDBDSNWithSafeSSLMode(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "postgres://u:p@db:5432/nms?sslmode=verify-full")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected URL-style DB_DSN with safe sslmode to pass, got %v", err)
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsPlainSMTPOverride(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey3")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error when NMS_SMTP_ALLOW_PLAINTEXT is enabled in production")
	}
}

func TestValidateRuntimeSecurity_ProductionAllowsNoSMTPConfig(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey4")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_FROM", "")

	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected valid production config without smtp, got %v", err)
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsPartialSMTPConfig(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey5")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for partial SMTP config")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsInvalidSMTPPort(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey6")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "bad-port")
	t.Setenv("SMTP_FROM", "nms@example.com")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for invalid SMTP_PORT")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsNonTLSStandardSMTPPort(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey8")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "25")
	t.Setenv("SMTP_FROM", "nms@example.com")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for insecure SMTP_PORT in production")
	}
}

func TestValidateRuntimeSecurity_ProductionAcceptsTLSStandardSMTPPorts(t *testing.T) {
	for _, port := range []string{"465", "587"} {
		t.Run("port_"+port, func(t *testing.T) {
			knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey9")
			t.Setenv("NMS_ENV", "production")
			t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
			t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
			t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
			t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
			t.Setenv("NMS_ALLOW_NO_AUTH", "")
			t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
			t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
			t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
			t.Setenv("NMS_ENFORCE_HTTPS", "true")
			t.Setenv("SMTP_HOST", "smtp.example.com")
			t.Setenv("SMTP_PORT", port)
			t.Setenv("SMTP_FROM", "nms@example.com")

			if err := ValidateRuntimeSecurity(); err != nil {
				t.Fatalf("expected allowed TLS SMTP port %s, got %v", port, err)
			}
		})
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsInvalidSMTPFrom(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey14")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "not-an-email")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for invalid SMTP_FROM")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsPartialSMTPAuth(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "nms@example.com")
	t.Setenv("SMTP_USER", "mailer")
	t.Setenv("SMTP_PASS", "")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for partial SMTP auth config")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsSMTPHostURL(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp://smtp.example.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "nms@example.com")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for SMTP_HOST URL-like value")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsSMTPHostWithPort(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com:587")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "nms@example.com")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for SMTP_HOST with embedded port")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsTooShortSessionSecret(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey15")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "short")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for too short NMS_SESSION_SECRET")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsEmptyKnownHostsFile(t *testing.T) {
	knownHosts := writeTempFile(t, "\n# comment only\n\n")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for empty known_hosts file")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsMalformedKnownHostsFile(t *testing.T) {
	knownHosts := writeTempFile(t, "this-is-not-known-hosts-format\n")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for malformed known_hosts file")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsRelativeKnownHostsPath(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "known_hosts")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for relative known_hosts path")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsMissingKnownHostsFile(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "/tmp/does-not-exist-known-hosts")
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for missing known_hosts file")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsUnreadableSecretFileRef(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey7")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_SESSION_SECRET_FILE", "/tmp/does-not-exist-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for unreadable NMS_SESSION_SECRET_FILE")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsRelativeSecretFileRef(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_SESSION_SECRET_FILE", "session.secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for relative NMS_SESSION_SECRET_FILE")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsRelativeSMTPSecretFileRef(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")
	t.Setenv("SMTP_FROM", "")
	t.Setenv("SMTP_PASS_FILE", "smtp.pass")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for relative SMTP_PASS_FILE")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsTooShortDBEncryptionKey(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey10")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "short")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for too short NMS_DB_ENCRYPTION_KEY")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsDBDSNWithoutHost(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey11")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "port=5432 user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for DB_DSN without host")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsDBDSNWithoutUser(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for DB_DSN without user")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsDBDSNWithEmptyRequiredValues(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host= user= dbname= sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for DB_DSN with empty required values")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsDBDSNWithoutDBName(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey12")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=5432 user=u password=p sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for DB_DSN without dbname")
	}
}

func TestValidateRuntimeSecurity_ProductionAcceptsURLStyleDBDSN(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKey13")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "postgres://u:p@db:5432/nms?sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected URL-style DB_DSN to pass, got %v", err)
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsURLStyleDBDSNWithoutUser(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "postgres://db:5432/nms?sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for URL-style DB_DSN without user")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsDBDSNWithInvalidPort(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "host=db port=bad user=u password=p dbname=nms sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for invalid DB_DSN port")
	}
}

func TestValidateRuntimeSecurity_ProductionRejectsURLStyleDBDSNWithInvalidPort(t *testing.T) {
	knownHosts := writeTempFile(t, "example-host ssh-ed25519 AAAA-test")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", knownHosts)
	t.Setenv("DB_DSN", "postgres://u:p@db:99999/nms?sslmode=require")
	t.Setenv("NMS_ALLOW_NO_AUTH", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_SMTP_ALLOW_PLAINTEXT", "")
	t.Setenv("NMS_ENFORCE_HTTPS", "true")

	if err := ValidateRuntimeSecurity(); err == nil {
		t.Fatal("expected error for URL-style DB_DSN invalid port")
	}
}
