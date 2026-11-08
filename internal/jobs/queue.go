package jobs

import (
	"context"
	"fmt"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

// Queue — обёртка над metadata.Repository для работы с очередью задач.
type Queue struct {
	repo metadata.Repository
}

// NewQueue создает очередь задач.
func NewQueue(repo metadata.Repository) *Queue {
	return &Queue{repo: repo}
}

// Enqueue ставит задачу в очередь с idempotency_key.
// Если задача с таким ключом уже есть — возвращает её ID (без дублирования).
func (q *Queue) Enqueue(ctx context.Context, jobType string, payload []byte, idempotencyKey string) (int64, error) {
	pgRepo, ok := q.repo.(*metadata.PostgresRepo)
	if !ok {
		return 0, fmt.Errorf("repository is not PostgresRepo")
	}

	// 1. Проверяем, есть ли задача с таким idempotency_key
	if idempotencyKey != "" {
		var existingID int64
		err := pgRepo.GetPool().QueryRow(ctx,
			"SELECT id FROM jobs WHERE idempotency_key = $1", idempotencyKey).Scan(&existingID)
		if err == nil {
			// Задача уже есть — возвращаем её ID
			return existingID, nil
		}
	}

	// 2. Вставляем новую задачу
	var id int64
	err := pgRepo.GetPool().QueryRow(ctx,
		`INSERT INTO jobs (type, payload, idempotency_key) 
		 VALUES ($1, $2, $3) 
		 RETURNING id`,
		jobType, payload, idempotencyKey).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("enqueue job: %w", err)
	}

	return id, nil
}
