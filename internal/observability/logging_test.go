package observability

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogging(t *testing.T) {
	// 1. Создаем in-memory observer для логов (zaptest/observer)
	core, observedLogs := observer.New(zap.InfoLevel)

	// 2. Создаем логгер на основе observer
	logger := zap.New(core)

	// 3. Пишем лог с полями
	logger.Info("test message",
		zap.String("key", "value"),
		zap.Int("number", 42),
	)

	// 4. Проверяем, что лог записан
	if observedLogs.Len() != 1 {
		t.Fatalf("expected 1 log entry, got %d", observedLogs.Len())
	}

	// 5. Проверяем сообщение
	entry := observedLogs.All()[0]
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got '%s'", entry.Message)
	}

	// 6. Проверяем поля
	fields := entry.ContextMap()
	if fields["key"] != "value" {
		t.Errorf("expected field 'key'='value', got '%v'", fields["key"])
	}
	if fields["number"] != int64(42) {
		t.Errorf("expected field 'number'=42, got '%v'", fields["number"])
	}
}
