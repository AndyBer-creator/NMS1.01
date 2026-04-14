package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	if err := validateDBEncryptionKeyStrength(EnvOrFile("NMS_DB_ENCRYPTION_KEY")); err != nil {
		return err
	}
	for _, name := range []string{"NMS_SESSION_SECRET", "NMS_DB_ENCRYPTION_KEY", "DB_DSN"} {
		if err := validateOptionalSecretFileVar(name); err != nil {
			return err
		}
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
	if err := validateKnownHostsFile(EnvOrFile("NMS_TERMINAL_SSH_KNOWN_HOSTS")); err != nil {
		return err
	}
	if !envEnabled("NMS_ENFORCE_HTTPS") {
		return fmt.Errorf("production guardrail: NMS_ENFORCE_HTTPS must be enabled")
	}
	if err := validateProductionSMTPConfig(); err != nil {
		return err
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

func validateProductionSMTPConfig() error {
	host := strings.TrimSpace(EnvOrFile("SMTP_HOST"))
	port := strings.TrimSpace(EnvOrFile("SMTP_PORT"))
	from := strings.TrimSpace(EnvOrFile("SMTP_FROM"))

	if host == "" && port == "" && from == "" {
		return nil
	}
	if host == "" || port == "" || from == "" {
		return fmt.Errorf("production guardrail: SMTP_HOST, SMTP_PORT and SMTP_FROM must be set together")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("production guardrail: SMTP_PORT must be numeric in range 1..65535")
	}
	if p != 465 && p != 587 {
		return fmt.Errorf("production guardrail: SMTP_PORT must be 465 (SMTPS) or 587 (STARTTLS)")
	}
	return nil
}

func validateOptionalSecretFileVar(name string) error {
	path := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("production guardrail: %s_FILE is not readable: %w", name, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("production guardrail: %s_FILE is empty", name)
	}
	return nil
}

func validateKnownHostsFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must be set")
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS is not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must point to a file, got directory %q", filepath.Clean(p))
	}
	return nil
}

func validateDBEncryptionKeyStrength(secret string) error {
	s := strings.TrimSpace(secret)
	// DB secrets are derived into AES key material; enforce a minimum input length
	// to avoid trivial, low-entropy production passphrases.
	if len(s) < 8 {
		return fmt.Errorf("production guardrail: NMS_DB_ENCRYPTION_KEY must be at least 8 characters")
	}
	return nil
}
