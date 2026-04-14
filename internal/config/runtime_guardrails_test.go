package config

import "testing"

func TestValidateRuntimeSecurity_NonProductionNoop(t *testing.T) {
	t.Setenv("NMS_ENV", "docker")
	t.Setenv("NMS_SESSION_SECRET", "")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "")
	if err := ValidateRuntimeSecurity(); err != nil {
		t.Fatalf("expected no error for non-production, got %v", err)
	}
}

func TestValidateRuntimeSecurity_ProductionRequiresSecretsAndSafeFlags(t *testing.T) {
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "/run/secrets/nms_terminal_known_hosts")
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
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "/run/secrets/nms_terminal_known_hosts")
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
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "/run/secrets/nms_terminal_known_hosts")
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
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_SECRET", "prod-session-secret")
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "prod-db-enc")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "/run/secrets/nms_terminal_known_hosts")
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
