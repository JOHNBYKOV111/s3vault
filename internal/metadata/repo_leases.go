package metadata

import (
	"context"
	"fmt"
	"time"
)

// AcquireLease пытается взять блокировку (lease) на указанное время.
// Если блокировка уже занята другим holder'ом и не истекла — возвращает ErrLeaseTaken.
func (r *PostgresRepo) AcquireLease(ctx context.Context, leaseID, holder string, ttl time.Duration) error {
	// 1. Пытаемся вставить новую блокировку (если её нет)
	// 2. Если она уже есть, обновляем только если она истекла или принадлежит нам
	query := `
		INSERT INTO leases (id, holder, expires_at)
		VALUES ($1, $2, NOW() + $3::interval)
		ON CONFLICT (id) DO UPDATE
		SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
		WHERE leases.expires_at < NOW() OR leases.holder = EXCLUDED.holder
		RETURNING id
	`

	var id string
	err := r.pool.QueryRow(ctx, query, leaseID, holder, ttl.String()).Scan(&id)
	if err != nil {
		// Если RETURNING не вернул строку (WHERE не совпал), значит блокировка занята
		return ErrLeaseTaken
	}

	return nil
}

// ReleaseLease освобождает блокировку, если она принадлежит указанному holder'у.
func (r *PostgresRepo) ReleaseLease(ctx context.Context, leaseID, holder string) error {
	query := `DELETE FROM leases WHERE id = $1 AND holder = $2`

	tag, err := r.pool.Exec(ctx, query, leaseID, holder)
	if err != nil {
		return fmt.Errorf("release lease: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("lease not found or not owned by holder")
	}

	return nil
}
