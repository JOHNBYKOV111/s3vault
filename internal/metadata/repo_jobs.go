package metadata

import (
	"context"
	"fmt"
	"time"
)

// Job — задача в очереди.
type Job struct {
	ID        int64
	Type      string
	Payload   []byte
	Status    string
	Attempts  int
	CreatedAt time.Time
}

// Enqueue добавляет задачу в очередь и возвращает её ID.
func (r *PostgresRepo) Enqueue(ctx context.Context, jobType string, payload []byte) (int64, error) {
	query := `
		INSERT INTO jobs (type, payload)
		VALUES ($1, $2)
		RETURNING id
	`

	var id int64
	err := r.pool.QueryRow(ctx, query, jobType, payload).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("enqueue job: %w", err)
	}
	return id, nil
}

// Dequeue берет одну задачу из очереди и помечает её как "processing".
// Использует FOR UPDATE SKIP LOCKED, чтобы два воркера не взяли одну задачу.
func (r *PostgresRepo) Dequeue(ctx context.Context, workerID string) (*Job, error) {
	query := `
		UPDATE jobs
		SET status = 'processing', updated_at = NOW()
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'pending'
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, payload, status, attempts, created_at
	`

	var j Job
	err := r.pool.QueryRow(ctx, query).Scan(
		&j.ID, &j.Type, &j.Payload, &j.Status, &j.Attempts, &j.CreatedAt,
	)
	if err != nil {
		// Если задач нет, возвращаем nil (без ошибки)
		return nil, nil
	}

	return &j, nil
}

// CompleteJob помечает задачу как выполненную.
func (r *PostgresRepo) CompleteJob(ctx context.Context, jobID int64) error {
	query := `UPDATE jobs SET status = 'done', updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, jobID)
	return err
}

// FailJob помечает задачу как упавшую и увеличивает счетчик попыток.
// Если попыток больше maxAttempts, задача помечается как 'failed' (dead-letter).
func (r *PostgresRepo) FailJob(ctx context.Context, jobID int64, errMsg string, maxAttempts int) error {
	query := `
		UPDATE jobs
		SET status = CASE WHEN attempts + 1 >= $3 THEN 'failed' ELSE 'pending' END,
		    attempts = attempts + 1,
		    last_error = $1,
		    updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.pool.Exec(ctx, query, errMsg, jobID, maxAttempts)
	return err
}
