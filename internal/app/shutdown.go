package app

import (
	"context"
	"net/http"
)

// ShutdownServer выполняет graceful shutdown HTTP-сервера
func ShutdownServer(ctx context.Context, srv *http.Server) error {
	// Shutdown мягко выключает сервер: перестает принимать новые соединения
	// и ждет, пока активные завершатся, или пока не истечет контекст.
	return srv.Shutdown(ctx)
}
