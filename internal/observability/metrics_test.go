package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsMiddleware(t *testing.T) {
	// Сбрасываем счетчик перед тестом
	httpRequestsTotal.Reset()

	// Создаем тестовый запрос
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Оборачиваем пустой хендлер в нашу middleware
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	// Проверяем, что счетчик увеличился
	val := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/test", "200"))
	if val != 1 {
		t.Errorf("expected counter to be 1, got %f", val)
	}
}

func TestMetricsUSE(t *testing.T) {
	// USE-метрики регистрируются в изолированном реестре через init().
	// Проверяем, что process_* и go_* присутствуют именно в нашем реестре.
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	found := map[string]bool{
		"process_cpu_seconds_total":     false,
		"process_resident_memory_bytes": false,
		"go_goroutines":                 false,
	}
	for _, mf := range families {
		if _, ok := found[mf.GetName()]; ok {
			found[mf.GetName()] = true
		}
	}
	for name, ok := range found {
		if !ok {
			t.Errorf("expected USE metric %q to be registered", name)
		}
	}
}

func TestMetricsBusiness(t *testing.T) {
	// gauge: dedup ratio
	s3DedupRatio.Set(0.42)
	if v := testutil.ToFloat64(s3DedupRatio); v != 0.42 {
		t.Errorf("expected s3_dedup_ratio=0.42, got %f", v)
	}

	// gauge: jobs queue depth
	s3JobsQueueDepth.Set(7)
	if v := testutil.ToFloat64(s3JobsQueueDepth); v != 7 {
		t.Errorf("expected s3_jobs_queue_depth=7, got %f", v)
	}

	// counter: blobs total
	s3BlobsTotal.Add(3)
	if v := testutil.ToFloat64(s3BlobsTotal); v != 3 {
		t.Errorf("expected s3_blobs_total=3, got %f", v)
	}
}
