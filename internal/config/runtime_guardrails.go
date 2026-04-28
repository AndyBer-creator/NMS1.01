package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh/knownhosts"
)

func envEnabled(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func isProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("NMS_ENV")), "production")
}

func isRuntimeHardeningRequired() bool {
	if isProductionEnv() {
		return true
	}
	return envEnabled("NMS_REQUIRE_PROD_GUARDRAILS")
}

// RuntimeSecurityRole scopes production guardrails to process responsibilities.
type RuntimeSecurityRole string

const (
	RuntimeSecurityRoleAPI          RuntimeSecurityRole = "api"
	RuntimeSecurityRoleWorker       RuntimeSecurityRole = "worker"
	RuntimeSecurityRoleTrapReceiver RuntimeSecurityRole = "trap-receiver"
)

// ValidateRuntimeSecurity applies strict guardrails for production mode.
// Backward-compatible default: API process.
func ValidateRuntimeSecurity() error {
	return ValidateRuntimeSecurityFor(RuntimeSecurityRoleAPI)
}

func ValidateRuntimeSecurityFor(role RuntimeSecurityRole) error {
	if !isRuntimeHardeningRequired() {
		return nil
	}

	if strings.TrimSpace(EnvOrFile("NMS_DB_ENCRYPTION_KEY")) == "" {
		return fmt.Errorf("production guardrail: NMS_DB_ENCRYPTION_KEY must be set")
	}
	if err := validateDBEncryptionKeyStrength(EnvOrFile("NMS_DB_ENCRYPTION_KEY")); err != nil {
		return err
	}
	for _, name := range []string{
		"NMS_SESSION_SECRET",
		"NMS_DB_ENCRYPTION_KEY",
		"NMS_ALERT_WEBHOOK_TOKEN",
		"NMS_ITSM_INBOUND_TOKEN",
		"DB_DSN",
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_USER",
		"SMTP_PASS",
		"SMTP_FROM",
	} {
		if err := validateOptionalSecretFileVar(name); err != nil {
			return err
		}
	}

	for _, name := range roleForbiddenFlags(role) {
		if envEnabled(name) {
			return fmt.Errorf("production guardrail: %s must be disabled", name)
		}
	}

	dsn := strings.ToLower(strings.TrimSpace(EnvOrFile("DB_DSN")))
	if dsn != "" && !hasSafeDBSSLMode(dsn) {
		return fmt.Errorf("production guardrail: DB_DSN must set sslmode=require|verify-ca|verify-full")
	}
	if err := validateDBDSNShape(EnvOrFile("DB_DSN")); err != nil {
		return err
	}
	if err := validateProductionSMTPConfig(); err != nil {
		return err
	}
	if role == RuntimeSecurityRoleAPI {
		if strings.TrimSpace(EnvOrFile("NMS_SESSION_SECRET")) == "" {
			return fmt.Errorf("production guardrail: NMS_SESSION_SECRET must be set")
		}
		if err := validateSessionSecretStrength(EnvOrFile("NMS_SESSION_SECRET")); err != nil {
			return err
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
	}

	return nil
}

func roleForbiddenFlags(role RuntimeSecurityRole) []string {
	switch role {
	case RuntimeSecurityRoleWorker, RuntimeSecurityRoleTrapReceiver:
		return []string{"NMS_SMTP_ALLOW_PLAINTEXT"}
	case RuntimeSecurityRoleAPI:
		fallthrough
	default:
		return []string{
			"NMS_ALLOW_NO_AUTH",
			"NMS_TERMINAL_ALLOW_INSECURE_ORIGIN",
			"NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY",
			"NMS_SMTP_ALLOW_PLAINTEXT",
		}
	}
}

func hasSafeDBSSLMode(dsn string) bool {
	mode := extractDBSSLMode(strings.TrimSpace(dsn))
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

func extractDBSSLMode(dsn string) string {
	if dsn == "" {
		return ""
	}
	lower := strings.ToLower(dsn)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return ""
		}
		return u.Query().Get("sslmode")
	}
	return parseDSNKeyValueFields(dsn)["sslmode"]
}

func validateProductionSMTPConfig() error {
	host := strings.TrimSpace(EnvOrFile("SMTP_HOST"))
	port := strings.TrimSpace(EnvOrFile("SMTP_PORT"))
	from := strings.TrimSpace(EnvOrFile("SMTP_FROM"))
	user := strings.TrimSpace(EnvOrFile("SMTP_USER"))
	pass := strings.TrimSpace(EnvOrFile("SMTP_PASS"))

	if host == "" && port == "" && from == "" {
		return nil
	}
	if host == "" || port == "" || from == "" {
		return fmt.Errorf("production guardrail: SMTP_HOST, SMTP_PORT and SMTP_FROM must be set together")
	}
	if err := validateSMTPHost(host); err != nil {
		return err
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("production guardrail: SMTP_PORT must be numeric in range 1..65535")
	}
	if p != 465 && p != 587 {
		return fmt.Errorf("production guardrail: SMTP_PORT must be 465 (SMTPS) or 587 (STARTTLS)")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("production guardrail: SMTP_FROM must be a valid email address")
	}
	if (user == "") != (pass == "") {
		return fmt.Errorf("production guardrail: SMTP_USER and SMTP_PASS must be set together")
	}
	return nil
}

func validateSMTPHost(host string) error {
	h := strings.TrimSpace(host)
	if h == "" {
		return fmt.Errorf("production guardrail: SMTP_HOST must be set")
	}
	if strings.Contains(h, "://") || strings.Contains(h, "/") {
		return fmt.Errorf("production guardrail: SMTP_HOST must be a hostname or IP, not URL")
	}
	if strings.ContainsAny(h, " \t\r\n") {
		return fmt.Errorf("production guardrail: SMTP_HOST must not contain whitespace")
	}
	if _, _, err := net.SplitHostPort(h); err == nil {
		return fmt.Errorf("production guardrail: SMTP_HOST must not include port; use SMTP_PORT")
	}
	return nil
}

func validateOptionalSecretFileVar(name string) error {
	path := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("production guardrail: %s_FILE must be an absolute path", name)
	}
	// #nosec -- file path is validated as absolute and used only for reading required secret material.
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
	if !filepath.IsAbs(p) {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must be an absolute path")
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS is not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must point to a file, got directory %q", filepath.Clean(p))
	}
	// #nosec -- path is validated as absolute and must point to a readable known_hosts file.
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS is not readable: %w", err)
	}
	if !hasKnownHostsEntries(string(b)) {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS must contain at least one host key entry")
	}
	if _, err := knownhosts.New(p); err != nil {
		return fmt.Errorf("production guardrail: NMS_TERMINAL_SSH_KNOWN_HOSTS has invalid format: %w", err)
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

func validateSessionSecretStrength(secret string) error {
	s := strings.TrimSpace(secret)
	if len(s) < 12 {
		return fmt.Errorf("production guardrail: NMS_SESSION_SECRET must be at least 12 characters")
	}
	return nil
}

func hasKnownHostsEntries(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		return true
	}
	return false
}

func validateDBDSNShape(dsn string) error {
	raw := strings.TrimSpace(dsn)
	if raw == "" {
		return fmt.Errorf("production guardrail: DB_DSN must be set")
	}
	lower := strings.ToLower(raw)
	// URL DSN: postgres://user:pass@host:5432/dbname?sslmode=require
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("production guardrail: DB_DSN is invalid URL: %w", err)
		}
		if u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
			return fmt.Errorf("production guardrail: DB_DSN must include user")
		}
		if strings.TrimSpace(u.Hostname()) == "" {
			return fmt.Errorf("production guardrail: DB_DSN must include host")
		}
		if err := validateOptionalPortValue(u.Port(), "DB_DSN"); err != nil {
			return err
		}
		db := strings.Trim(u.Path, "/")
		if db == "" {
			return fmt.Errorf("production guardrail: DB_DSN must include dbname in path")
		}
		return nil
	}

	// key=value DSN.
	fields := parseDSNKeyValueFields(raw)
	if strings.TrimSpace(fields["host"]) == "" {
		return fmt.Errorf("production guardrail: DB_DSN must include host")
	}
	if strings.TrimSpace(fields["user"]) == "" {
		return fmt.Errorf("production guardrail: DB_DSN must include user")
	}
	if strings.TrimSpace(fields["dbname"]) == "" {
		return fmt.Errorf("production guardrail: DB_DSN must include dbname")
	}
	if err := validateOptionalPortValue(fields["port"], "DB_DSN"); err != nil {
		return err
	}
	return nil
}

func parseDSNKeyValueFields(raw string) map[string]string {
	out := make(map[string]string)
	for _, token := range strings.Fields(raw) {
		kv := strings.SplitN(token, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		if k != "" {
			out[k] = v
		}
	}
	return out
}

func validateOptionalPortValue(port, label string) error {
	p := strings.TrimSpace(port)
	if p == "" {
		return nil
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("production guardrail: %s port must be numeric in range 1..65535", label)
	}
	return nil
}
