package s3api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/metadata"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestChaosContextCancel(t *testing.T) {
	// 1. Поднимаем Postgres
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("s3vault"),
		postgres.WithUsername("s3vault"),
		postgres.WithPassword("s3vault"),
		// Ждём именно готовность БД принимать запросы (лог "ready to accept
		// connections"), а не просто открытый порт. Порт открывается раньше,
		// чем Postgres готов, и тогда ping падает с "the database system is
		// starting up". Таймаут — 120с, чтобы пережить холодный старт под
		// нагрузкой (несколько контейнеров параллельно в go test ./...).
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(120*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}
	defer func() { _ = pgContainer.Terminate(ctx) }()

	connString, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	repo, err := metadata.NewPostgresRepo(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer repo.Close()

	migration := `
		CREATE TABLE tenants (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE buckets (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(tenant_id, name));
		CREATE TABLE objects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE, key TEXT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, content_type TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(bucket_id, key));
	`
	if _, err := repo.GetPool().Exec(ctx, migration); err != nil {
		t.Fatalf("failed to apply migration: %v", err)
	}

	// Создаем бакет
	tenantID := "00000000-0000-0000-0000-000000000001"
	_, err = repo.GetPool().Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, 'chaos-tenant')", tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	_, err = repo.GetPool().Exec(ctx, "INSERT INTO buckets (tenant_id, name) VALUES ($1, 'chaos-bucket')", tenantID)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// 2. Создаем handler
	fsStore, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create fs store: %v", err)
	}
	handler := NewObjectHandler(fsStore, repo)

	// 3. Создаем pipe (медленный reader)
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	// 4. Запускаем PUT в горутине
	done := make(chan error, 1)
	go func() {
		req := httptest.NewRequest("PUT", "/chaos-bucket/bigfile.bin", pr)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			done <- io.ErrUnexpectedEOF
			return
		}
		done <- nil
	}()

	// 5. Пишем немного данных и отменяем контекст
	_, _ = pw.Write([]byte("Hello, Chaos!"))
	// (В реальном chaos-тесте мы бы убили процесс, но здесь мы просто закрываем pipe)
	pw.CloseWithError(io.ErrUnexpectedEOF) // имитация отмены

	// 6. Ждем завершения (с таймаутом)
	select {
	case err := <-done:
		if err == nil {
			t.Log("PUT completed (unexpected, but possible)")
		} else {
			t.Logf("PUT failed as expected: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for PUT")
	}

	// 7. Проверяем, что объект НЕ создан в базе (или создан, но не «полузаписан»)
	var count int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM objects WHERE key = 'bigfile.bin'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query objects: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 objects after canceled PUT, got %d", count)
	}
}
