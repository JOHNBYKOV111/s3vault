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

func setupQueueTest(t *testing.T) (*metadata.PostgresRepo, func()) {
	t.Helper()

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("s3vault"),
		postgres.WithUsername("s3vault"),
		postgres.WithPassword("s3vault"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second), // УВЕЛИЧЕНО до 30 секунд
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

func TestIdempotencyKey(t *testing.T) {
	repo, cleanup := setupQueueTest(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Ставим задачу с idempotency_key
	payload := []byte(`{"bucket":"test","key":"test.txt"}`)
	_, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	// 2. Проверяем, что задача в очереди
	var count int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM jobs").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query jobs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 job, got %d", count)
	}

	// 3. Dequeue (берем задачу)
	job, err := repo.Dequeue(ctx, "worker-1")
	if err != nil {
		t.Fatalf("failed to dequeue: %v", err)
	}
	if job == nil {
		t.Fatal("expected job, got nil")
	}

	// 4. Проверяем, что статус изменился на processing
	var status string
	err = repo.GetPool().QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", job.ID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job status: %v", err)
	}
	if status != "processing" {
		t.Errorf("expected status 'processing', got '%s'", status)
	}
}
