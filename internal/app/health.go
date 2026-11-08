package app

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

var isReady atomic.Bool

func init() {
	isReady.Store(true) // По умолчанию готовы
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

func ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	if !isReady.Load() {
		http.Error(w, `{"status":"not ready"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ready"}`)
}

// SetReady позволяет менять состояние (для тестов и graceful shutdown)
func SetReady(ready bool) {
	isReady.Store(ready)
}
