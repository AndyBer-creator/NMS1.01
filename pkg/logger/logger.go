package logger

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	logrus "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	*logrus.Logger
	serviceName string
}

func New(serviceName string) *Logger {
	log := logrus.New()

	// Продакшен ротация логов
	log.Out = &lumberjack.Logger{
		Filename:   filepath.Join("/app/logs", serviceName+".log"),
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
		LocalTime:  true,
	}

	// JSON формат для ELK/parsers
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			return f.Function, f.File + ":" + strconv.Itoa(f.Line)
		},
	})

	log.SetLevel(logrus.InfoLevel)
	log.SetReportCaller(true)

	// Создать папку logs
	_ = os.MkdirAll("/app/logs", 0755)

	return &Logger{Logger: log, serviceName: serviceName}
}

func (l *Logger) WithDevice(ip, name string) *logrus.Entry {
	return l.WithFields(logrus.Fields{
		"service":     l.serviceName,
		"device_ip":   ip,
		"device_name": name,
	})
}
