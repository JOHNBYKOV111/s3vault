package observability

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// InitLogger создает production-логгер (JSON) с уровнем логирования.
// Возвращает логгер и функцию для синхронизации (вызывать при shutdown).
func InitLogger(level string) (*zap.Logger, error) {
	// 1. Парсим уровень логирования
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel // дефолт
	}

	// 2. Создаем production-конфиг (JSON, сэмплирование)
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// 3. Создаем логгер
	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return logger, nil
}
