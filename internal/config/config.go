package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config contains runtime settings loaded from file and environment.
type Config struct {
	HTTP struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"http"`

	SNMP struct {
		Port    uint16 `mapstructure:"port"`
		Timeout int    `mapstructure:"timeout"` // seconds
		Retries int    `mapstructure:"retries"`
	} `mapstructure:"snmp"`

	Paths struct {
		// Directory for MIB files uploaded from UI.
		MibUploadDir string `mapstructure:"mib_upload_dir"`
		// Extra directories for snmptranslate; if empty, defaults are derived.
		MibSearchDirs []string `mapstructure:"mib_search_dirs"`
	} `mapstructure:"paths"`

	DB struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"db"`
}

// Load reads config.yaml (if present), applies env overrides and defaults.
func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	// Config file is optional: defaults + env still work when absent.
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config.yaml: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config.yaml: %w", err)
	}

	// DSN from DB_DSN_FILE/DB_DSN secret sources.
	if dsn := EnvOrFile("DB_DSN"); dsn != "" {
		cfg.DB.DSN = dsn
	}
	if cfg.DB.DSN == "" {
		return nil, fmt.Errorf("DB_DSN must be set in environment")
	}
	// Defaults for non-secret settings.
	if cfg.HTTP.Addr == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.SNMP.Port == 0 {
		cfg.SNMP.Port = 161
	}
	if cfg.SNMP.Timeout == 0 {
		cfg.SNMP.Timeout = 3
	}
	if cfg.SNMP.Retries == 0 {
		cfg.SNMP.Retries = 1
	}
	if cfg.Paths.MibUploadDir == "" {
		if os.Getenv("NMS_ENV") == "docker" {
			cfg.Paths.MibUploadDir = "/app/mibs/uploads"
		} else {
			cfg.Paths.MibUploadDir = filepath.Join("mibs", "uploads")
		}
	}
	if v := os.Getenv("MIB_UPLOAD_DIR"); v != "" {
		cfg.Paths.MibUploadDir = v
	}
	return &cfg, nil
}

// EnvDurationOrDefault parses positive duration from env or returns fallback.
func EnvDurationOrDefault(name string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// MIBSearchDirs returns effective MIBDIRS list for snmptranslate lookups.
func MIBSearchDirs(cfg *Config) []string {
	var dirs []string
	if cfg.Paths.MibUploadDir != "" {
		dirs = append(dirs, filepath.Clean(cfg.Paths.MibUploadDir))
	}
	for _, d := range cfg.Paths.MibSearchDirs {
		d = strings.TrimSpace(d)
		if d != "" {
			dirs = append(dirs, filepath.Clean(d))
		}
	}
	if len(cfg.Paths.MibSearchDirs) > 0 {
		return dedupeDirList(dirs)
	}
	base := filepath.Dir(cfg.Paths.MibUploadDir)
	for _, sub := range []string{"public", "vendor"} {
		dirs = append(dirs, filepath.Join(base, sub))
	}
	return dedupeDirList(dirs)
}

// dedupeDirList removes empty and duplicate directory entries preserving order.
func dedupeDirList(dirs []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// SNMPClientConfig returns SNMP client settings in runtime-friendly types.
func (c *Config) SNMPClientConfig() (int, time.Duration, int) {
	return int(c.SNMP.Port), time.Duration(c.SNMP.Timeout) * time.Second, c.SNMP.Retries
}
