package metadata

import (
	"context"
	"testing"
	"time"
)

func TestJobsQueue(t *testing.T) {
	repo, cleanup := setupTestRepoWithContainer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Ставим задачу в очередь
	payload := []byte(`{"bucket":"test-bucket","key":"test-key"}`)
	jobID, err := repo.Enqueue(ctx, "test-job", payload)
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

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
	err = repo.GetPool().QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", job1.ID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job status: %v", err)
	}
	if status != "done" {
		t.Errorf("expected status 'done', got '%s'", status)
	}
}
