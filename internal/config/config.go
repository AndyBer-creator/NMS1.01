package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	HTTP struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"http"`

	SNMP struct {
		Port    uint16 `mapstructure:"port"`
		Timeout int    `mapstructure:"timeout"` // секунды
		Retries int    `mapstructure:"retries"`
	} `mapstructure:"snmp"`

	Paths struct {
		// Каталог для MIB, загружаемых через веб (файлы на диске; NMS по-прежнему оперирует числовыми OID).
		MibUploadDir string `mapstructure:"mib_upload_dir"`
		// Дополнительные каталоги для snmptranslate (если пусто — добавляются ../public и ../vendor относительно uploads).
		MibSearchDirs []string `mapstructure:"mib_search_dirs"`
	} `mapstructure:"paths"`

	DB struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"db"`
}

func Load() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	// Конфиг опционален: если файла нет, работаем с дефолтами и env
	_ = viper.ReadInConfig()

	var cfg Config
	_ = viper.Unmarshal(&cfg)

	// DSN из DB_DSN_FILE/DB_DSN (секреты).
	if dsn := EnvOrFile("DB_DSN"); dsn != "" {
		cfg.DB.DSN = dsn
	}
	if cfg.DB.DSN == "" {
		panic("DB_DSN must be set in environment")
	}
	// Дефолты для несекретных полей, если конфиг не задан
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
	return &cfg
}

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

// MIBSearchDirs — каталоги для MIBDIRS (snmptranslate): uploads, опционально из конфига, иначе public/vendor рядом с mibs/.
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

// ✅ НОВЫЙ МЕТОД для SNMP Worker
func (c *Config) SNMPClientConfig() (int, time.Duration, int) {
	return int(c.SNMP.Port), time.Duration(c.SNMP.Timeout) * time.Second, c.SNMP.Retries
}
