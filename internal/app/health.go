package app

import (
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
)

var isReady atomic.Bool

func init() {
	isReady.Store(true) // По умолчанию готовы
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, `{"status":"ok"}`); err != nil {
		// Заголовки уже отправлены, клиенту ошибку не сообщить.
		fmt.Fprintf(os.Stderr, "app: write healthz: %v\n", err)
	}
}

func ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	if !isReady.Load() {
		http.Error(w, `{"status":"not ready"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, `{"status":"ready"}`); err != nil {
		// Заголовки уже отправлены, клиенту ошибку не сообщить.
		fmt.Fprintf(os.Stderr, "app: write readyz: %v\n", err)
	}
}

// SetReady позволяет менять состояние (для тестов и graceful shutdown)
func SetReady(ready bool) {
	isReady.Store(ready)
}
