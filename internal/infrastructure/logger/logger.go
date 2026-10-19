package logger

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(serviceName, level string, production bool) (*zap.Logger, func(), error) {
	if err := os.MkdirAll("logs", 0755); err != nil {
		return nil, nil, fmt.Errorf("create logs directory: %w", err)
	}

	file, err := os.OpenFile(
		"logs/app.log",
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	zapLevel := parseLevel(level)

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "ts"
	encoderConfig.LevelKey = "level"
	encoderConfig.MessageKey = "msg"
	encoderConfig.CallerKey = "caller"

	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	encoder := zapcore.NewJSONEncoder(encoderConfig)

	fileWriter := zapcore.AddSync(file)
	consoleWriter := zapcore.AddSync(os.Stdout)
	var writer zapcore.WriteSyncer
	if production {
		writer = fileWriter
	} else {
		writer = consoleWriter
	}
	core := zapcore.NewCore(
		encoder,
		writer,
		zapLevel,
	)

	log := zap.New(
		core,
		zap.Fields(
			zap.String("service", serviceName),
		),
		zap.AddCaller(),
	)

	sync := func() {
		_ = log.Sync()
		_ = file.Close()
	}

	return log, sync, nil
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
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

	default:
		return zapcore.InfoLevel
	}
}
