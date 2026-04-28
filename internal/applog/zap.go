package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ResolveLogDir returns the directory for service log files.
// NMS_LOG_DIR overrides; otherwise docker uses /app/logs, else ./logs.
func ResolveLogDir() string {
	if d := strings.TrimSpace(os.Getenv("NMS_LOG_DIR")); d != "" {
		return d
	}
	if os.Getenv("NMS_ENV") == "docker" {
		return "/app/logs"
	}
	return "./logs"
}

func resolveLogLevel() zapcore.Level {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("NMS_LOG_LEVEL")))
	switch raw {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	case "", "info":
		return zapcore.InfoLevel
	default:
		return zapcore.InfoLevel
	}
}

// NewZapFile builds a production-style zap logger writing JSON lines to
// <logDir>/<serviceName>.log with rotation (lumberjack).
func NewZapFile(serviceName string) (*zap.Logger, error) {
	logDir := ResolveLogDir()
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", logDir, err)
	}

	hook := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, serviceName+".log"),
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
		LocalTime:  true,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(hook),
		resolveLogLevel(),
	)

	return zap.New(core, zap.AddCaller()), nil
}

// MustNewZapFile returns configured logger or panics on initialization failure.
func MustNewZapFile(serviceName string) *zap.Logger {
	logger, err := NewZapFile(serviceName)
	if err != nil {
		panic(err)
	}
	return logger
}
