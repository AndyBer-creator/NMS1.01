package config

import (
	"os"
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

	// DSN только из env
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
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
		cfg.SNMP.Timeout = 10
	}
	if cfg.SNMP.Retries == 0 {
		cfg.SNMP.Retries = 5
	}
	return &cfg
}

// ✅ НОВЫЙ МЕТОД для SNMP Worker
func (c *Config) SNMPClientConfig() (int, time.Duration, int) {
	return int(c.SNMP.Port), time.Duration(c.SNMP.Timeout) * time.Second, c.SNMP.Retries
}
