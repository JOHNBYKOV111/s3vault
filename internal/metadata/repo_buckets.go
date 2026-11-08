package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateBucket создает новый бакет для указанного арендатора.
func (r *PostgresRepo) CreateBucket(ctx context.Context, tenantID, name string) (*Bucket, error) {
	query := `
		INSERT INTO buckets (tenant_id, name)
		VALUES ($1, $2)
		RETURNING id, tenant_id, name, created_at
	`

	var b Bucket
	err := r.pool.QueryRow(ctx, query, tenantID, name).Scan(
		&b.ID, &b.TenantID, &b.Name, &b.CreatedAt,
	)
	if err != nil {
		// Проверяем на нарушение уникальности (бакет с таким именем уже есть)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert bucket: %w", err)
	}

	return &b, nil
}

// GetBucket возвращает бакет по имени и арендатору. Если не найден — ErrNotFound.
func (r *PostgresRepo) GetBucket(ctx context.Context, tenantID, name string) (*Bucket, error) {
	query := `
		SELECT id, tenant_id, name, created_at
		FROM buckets
		WHERE tenant_id = $1 AND name = $2
	`

	var b Bucket
	err := r.pool.QueryRow(ctx, query, tenantID, name).Scan(
		&b.ID, &b.TenantID, &b.Name, &b.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get bucket: %w", err)
	}

	return &b, nil
}

// ListBuckets возвращает список всех бакетов арендатора, отсортированный по имени.
func (r *PostgresRepo) ListBuckets(ctx context.Context, tenantID string) ([]*Bucket, error) {
	query := `
		SELECT id, tenant_id, name, created_at
		FROM buckets
		WHERE tenant_id = $1
		ORDER BY name
	`

	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query buckets: %w", err)
	}
	defer rows.Close()

	var buckets []*Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.ID, &b.TenantID, &b.Name, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bucket: %w", err)
		}
		buckets = append(buckets, &b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return buckets, nil
}

// DeleteBucket удаляет бакет. Если бакета не было — возвращает ErrNotFound.
func (r *PostgresRepo) DeleteBucket(ctx context.Context, tenantID, name string) error {
	query := `DELETE FROM buckets WHERE tenant_id = $1 AND name = $2`

	tag, err := r.pool.Exec(ctx, query, tenantID, name)
	if err != nil {
		return fmt.Errorf("delete bucket: %w", err)
	}

	// Если ни одна строка не удалена — бакета не было
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
