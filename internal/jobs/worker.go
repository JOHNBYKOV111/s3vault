package jobs

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

// Worker — пул воркеров для обработки задач из очереди.
type Worker struct {
	repo        metadata.Repository
	count       int
	maxAttempts int
	baseDelay   time.Duration
}

// NewWorker создает пул воркеров (без retry, maxAttempts = 1).
func NewWorker(repo metadata.Repository, count int) *Worker {
	return &Worker{
		repo:        repo,
		count:       count,
		maxAttempts: 1,
		baseDelay:   0,
	}
}

// NewWorkerWithRetry создает пул воркеров с retry (backoff).
func NewWorkerWithRetry(repo metadata.Repository, count, maxAttempts int, baseDelay time.Duration) *Worker {
	return &Worker{
		repo:        repo,
		count:       count,
		maxAttempts: maxAttempts,
		baseDelay:   baseDelay,
	}
}

// Start запускает N горутин, которые берут задачи из очереди.
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.count; i++ {
		go w.workerLoop(ctx, i)
	}
}

// workerLoop — это горутина, которая берет задачи из очереди и выполняет их.
func (w *Worker) workerLoop(ctx context.Context, id int) {
	log.Printf("worker %d started", id)

	for {
		select {
		case <-ctx.Done():
			log.Printf("worker %d stopped", id)
			return
		default:
		}

		// Берем задачу из очереди (SKIP LOCKED)
		job, err := w.repo.Dequeue(ctx, fmt.Sprintf("worker-%d", id))
		if err != nil {
			log.Printf("worker %d: dequeue error: %v", id, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if job == nil {
			// Нет задач — спим и пробуем снова
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Выполняем задачу (с retry)
		w.executeJob(ctx, id, job)
	}
}

// executeJob выполняет задачу с retry (backoff).
func (w *Worker) executeJob(ctx context.Context, workerID int, job *metadata.Job) {
	var lastErr error

	for attempt := 1; attempt <= w.maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("worker %d: processing job %d (type: %s, attempt: %d/%d)", workerID, job.ID, job.Type, attempt, w.maxAttempts)

		// Выполняем задачу
		lastErr = w.handleJob(ctx, job)

		if lastErr == nil {
			// Успех — помечаем как done
			if err := w.repo.CompleteJob(ctx, job.ID); err != nil {
				log.Printf("worker %d: failed to complete job %d: %v", workerID, job.ID, err)
			} else {
				log.Printf("worker %d: job %d done", workerID, job.ID)
			}
			return
		}

		// Ошибка — логируем и ждем backoff
		log.Printf("worker %d: job %d failed (attempt %d/%d): %v", workerID, job.ID, attempt, w.maxAttempts, lastErr)

		if attempt < w.maxAttempts {
			// Exponential backoff
			delay := w.baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}

	// Все попытки исчерпаны — помечаем как failed (DLQ)
	log.Printf("worker %d: job %d failed after %d attempts (DLQ)", workerID, job.ID, w.maxAttempts)
	if err := w.repo.FailJob(ctx, job.ID, fmt.Sprintf("failed after %d attempts: %v", w.maxAttempts, lastErr), w.maxAttempts); err != nil {
		log.Printf("worker %d: failed to mark job %d as failed: %v", workerID, job.ID, err)
	}
}

// handleJob выполняет задачу и возвращает ошибку (если задача должна упасть).
// В реальном коде здесь будет вызов store.Heal, cleanupStaleUploads и т.д.
// Для теста: если payload содержит "fail", возвращаем ошибку.
func (w *Worker) handleJob(ctx context.Context, job *metadata.Job) error {
	if strings.Contains(string(job.Payload), "fail") {
		return fmt.Errorf("simulated job failure")
	}
	return nil
}
