package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	// Создаем фейковый запрос к /healthz
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Создаем ResponseRecorder, который записывает ответ
	rr := httptest.NewRecorder()

	// Вызываем наш хендлер
	handler := http.HandlerFunc(HealthCheck)
	handler.ServeHTTP(rr, req)

	// Проверяем статус код
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Проверяем тело ответа
	expected := `{"status":"ok"}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestReadinessCheck(t *testing.T) {
	req, err := http.NewRequest("GET", "/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Проверяем состояние, когда система ГОТОВА
	SetReady(true)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ReadinessCheck)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected 200 when ready, got %d", status)
	}
	expectedReady := `{"status":"ready"}`
	if rr.Body.String() != expectedReady {
		t.Errorf("expected body %s, got %s", expectedReady, rr.Body.String())
	}

	// 2. Проверяем состояние, когда система НЕ ГОТОВА
	SetReady(false)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when not ready, got %d", status)
	}
}
