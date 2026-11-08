package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracing(t *testing.T) {
	// 1. Создаем in-memory exporter (tracetest)
	exporter := tracetest.NewInMemoryExporter()

	// 2. Создаем tracer provider с экспортером
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown tracer provider: %v", err)
		}
	}()

	// 3. Устанавливаем глобальный tracer
	otel.SetTracerProvider(tp)

	// 4. Создаем трейс
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.SetAttributes(attribute.String("key", "value"))
	span.End()

	// 5. Проверяем, что span есть в экспортере
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "test-span" {
		t.Errorf("expected span name 'test-span', got '%s'", spans[0].Name)
	}

	// 6. Проверяем атрибуты
	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "key" && attr.Value.AsString() == "value" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected attribute 'key=value' in span")
	}
}
