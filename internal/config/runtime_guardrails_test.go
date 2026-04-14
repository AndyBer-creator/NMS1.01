package config

import (
	"os"
	"testing"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
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
	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected no error for non-production, got %v", err)
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
