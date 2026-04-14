package config

import (
	"fmt"
	"os"
	"strings"
)

func envEnabled(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func isProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("NMS_ENV")), "production")
}

// ValidateRuntimeSecurity applies strict guardrails for production mode.
func ValidateRuntimeSecurity() error {
	if !isProductionEnv() {
		return nil
	}

	if strings.TrimSpace(EnvOrFile("NMS_SESSION_SECRET")) == "" {
		return fmt.Errorf("production guardrail: NMS_SESSION_SECRET must be set")
	}
	if strings.TrimSpace(EnvOrFile("NMS_DB_ENCRYPTION_KEY")) == "" {
		return fmt.Errorf("production guardrail: NMS_DB_ENCRYPTION_KEY must be set")
	}

	for _, name := range []string{
		"NMS_ALLOW_NO_AUTH",
		"NMS_TERMINAL_ALLOW_INSECURE_ORIGIN",
		"NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY",
		"NMS_SMTP_ALLOW_PLAINTEXT",
	} {
		if envEnabled(name) {
			return fmt.Errorf("production guardrail: %s must be disabled", name)
		}
	}

	dsn := strings.ToLower(strings.TrimSpace(EnvOrFile("DB_DSN")))
	if dsn != "" && !hasSafeDBSSLMode(dsn) {
		return fmt.Errorf("production guardrail: DB_DSN must set sslmode=require|verify-ca|verify-full")
	}
	if strings.TrimSpace(EnvOrFile("NMS_TERMINAL_SSH_KNOWN_HOSTS")) == "" {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must be set")
	}
	if !envEnabled("NMS_ENFORCE_HTTPS") {
		return fmt.Errorf("production guardrail: NMS_ENFORCE_HTTPS must be enabled")
	}

	return nil
}

func hasSafeDBSSLMode(dsn string) bool {
	// Covers key=value DSN and URL query DSN forms.
	for _, mode := range []string{"sslmode=require", "sslmode=verify-ca", "sslmode=verify-full"} {
		if strings.Contains(dsn, mode) {
			return true
		}
	}
	return false
}
