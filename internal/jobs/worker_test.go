package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/metadata"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupWorkerTest(t *testing.T) (*metadata.PostgresRepo, func()) {
	t.Helper()

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

	connString, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	repo, err := metadata.NewPostgresRepo(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to create repo: %v", err)
	}

	migration := `
		CREATE TABLE jobs (
			id BIGSERIAL PRIMARY KEY,
			type TEXT NOT NULL,
			payload JSONB NOT NULL,
			idempotency_key TEXT UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INT NOT NULL DEFAULT 0,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX idx_jobs_status ON jobs(status) WHERE status = 'pending';
	`
	if _, err := repo.GetPool().Exec(ctx, migration); err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}

	cleanup := func() {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
	}

	return repo, cleanup
}

func TestWorkerPool(t *testing.T) {
	repo, cleanup := setupWorkerTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Ставим задачу в очередь
	payload := []byte(`{"bucket":"test","key":"test.txt"}`)
	_, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	// 2. Запускаем воркер (1 горутина)
	worker := NewWorker(repo, 1)
	go worker.Start(ctx)

	// 3. Ждем, пока задача выполнится (с таймаутом)
	time.Sleep(500 * time.Millisecond)

	// 4. Проверяем, что задача помечена как done
	var status string
	err = repo.GetPool().QueryRow(ctx, "SELECT status FROM jobs LIMIT 1").Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if status != "done" {
		t.Errorf("expected status 'done', got '%s'", status)
	}
}

func TestWorkerRetry(t *testing.T) {
	repo, cleanup := setupWorkerTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Ставим задачу в очередь
	payload := []byte(`{"bucket":"test","key":"test.txt"}`)
	_, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	// 2. Запускаем воркер с retry (1 воркер, max 3 попытки, backoff 10 мс)
	worker := NewWorkerWithRetry(repo, 1, 3, 10*time.Millisecond)
	go worker.Start(ctx)

	// 3. Ждем, пока задача выполнится (с таймаутом)
	time.Sleep(500 * time.Millisecond)

	// 4. Проверяем, что задача помечена как done (или failed, если retry исчерпан)
	var status string
	err = repo.GetPool().QueryRow(ctx, "SELECT status FROM jobs LIMIT 1").Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if status != "done" && status != "failed" {
		t.Errorf("expected status 'done' or 'failed', got '%s'", status)
	}
}

func TestWorkerDLQ(t *testing.T) {
	repo, cleanup := setupWorkerTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Ставим задачу, которая всегда падает (payload содержит "fail")
	payload := []byte(`{"bucket":"test","key":"fail.txt"}`)
	_, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	// 2. Запускаем воркер с retry (1 воркер, max 1 попытка — сразу DLQ)
	worker := NewWorkerWithRetry(repo, 1, 1, 10*time.Millisecond)
	go worker.Start(ctx)

	// 3. Ждем, пока задача выполнится (с таймаутом)
	time.Sleep(500 * time.Millisecond)

	// 4. Проверяем, что задача помечена как failed (DLQ)
	var status string
	err = repo.GetPool().QueryRow(ctx, "SELECT status FROM jobs LIMIT 1").Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status 'failed' (DLQ), got '%s'", status)
	}
}
