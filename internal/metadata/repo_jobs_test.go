package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestJobsQueue(t *testing.T) {
	connString := "postgres://s3vault:s3vault@localhost:5433/s3vault?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	repo := &PostgresRepo{pool: pool}

	// 1. Ставим задачу в очередь
	payload := []byte(`{"bucket":"test-bucket","key":"test-key"}`)
	jobID, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM jobs WHERE id = $1", jobID)

	// 2. Воркер 1 берет задачу
	job1, err := repo.Dequeue(ctx, "worker-1")
	if err != nil {
		t.Fatalf("worker-1 failed to dequeue: %v", err)
	}
	if job1 == nil {
		t.Fatal("worker-1 got nil job")
	}
	if job1.ID != jobID {
		t.Errorf("expected job ID %d, got %d", jobID, job1.ID)
	}

	// 3. Воркер 2 пытается взять задачу (должен получить nil, так как она уже занята)
	job2, err := repo.Dequeue(ctx, "worker-2")
	if err != nil {
		t.Fatalf("worker-2 failed to dequeue: %v", err)
	}
	if job2 != nil {
		t.Errorf("worker-2 should not get a job, got ID %d", job2.ID)
	}

	// 4. Завершаем задачу
	err = repo.CompleteJob(ctx, job1.ID)
	if err != nil {
		t.Fatalf("failed to complete job: %v", err)
	}

	// 5. Проверяем, что задача помечена как done
	var status string
	err = pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", job1.ID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job status: %v", err)
	}
	if status != "done" {
		t.Errorf("expected status 'done', got '%s'", status)
	}
}
