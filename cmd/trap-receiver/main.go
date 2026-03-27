package main

import (
	// "context"
	"fmt"
	// "log"
	"net"
	"os"
	"path/filepath"

	//  "time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	// ✅ ТОТ ЖЕ logger что в worker!
	logger := setupLogger("nms-trap-receiver")
	defer logger.Sync()

	logger.Info("🚀 SNMP Trap Receiver v1 started")

	// UDP :1162 (стандартный SNMP trap порт)
	addr, err := net.ResolveUDPAddr("udp", ":1162")
	if err != nil {
		logger.Fatal("Resolve UDP failed", zap.Error(err))
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		logger.Fatal("Listen UDP failed", zap.Error(err))
	}
	defer conn.Close()

	logger.Info("Listening SNMP traps", zap.String("addr", "0.0.0.0:1162"))

	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logger.Warn("UDP read failed", zap.Error(err))
			continue
		}

		logger.Info("🔔 SNMP TRAP received",
			zap.String("from", remoteAddr.String()),
			zap.Int("bytes", n))

		// TODO: парсинг SNMP trap + БД + Telegram
		fmt.Printf("🔔 Trap: %s (%d bytes)\n", remoteAddr, n)
	}
}

// ✅ ТОТ ЖЕ setupLogger из worker!
func setupLogger(serviceName string) *zap.Logger {
	logDir := "./logs"
	if os.Getenv("NMS_ENV") == "docker" {
		logDir = "/app/logs"
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create log dir %s: %v", logDir, err))
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
		zapcore.InfoLevel,
	)

	return zap.New(core, zap.AddCaller())
}
