package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestGracefulShutdown(t *testing.T) {
	// Канал, который просигналит, что запрос дошел до сервера
	requestStarted := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Handler: mux,
	}

	// Создаем listener вручную, чтобы узнать реальный порт
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Запускаем сервер на этом listener
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("Serve(): %v", err)
		}
	}()

	// Получаем реальный адрес и порт
	addr := listener.Addr().String()

	respChan := make(chan int)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			respChan <- http.StatusInternalServerError
			return
		}
		respChan <- resp.StatusCode
	}()

	// ЖДЕМ, пока запрос точно не дойдет до сервера, но не дольше 2 секунд
	select {
	case <-requestStarted:
		// Отлично, запрос дошел
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request to start")
	}

	// Теперь вызываем Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = ShutdownServer(ctx, srv)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Проверяем, что запрос завершился успешно
	statusCode := <-respChan
	if statusCode != http.StatusOK {
		t.Errorf("expected slow request to finish with 200, got %d", statusCode)
	}
	fmt.Println("Test passed successfully")
}
