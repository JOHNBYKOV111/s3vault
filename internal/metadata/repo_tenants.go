package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateTenant создает нового арендатора (клиента).
func (r *PostgresRepo) CreateTenant(ctx context.Context, name string) (*Tenant, error) {
	query := `
		INSERT INTO tenants (name)
		VALUES ($1)
		RETURNING id, name, created_at
	`

	var t Tenant
	err := r.pool.QueryRow(ctx, query, name).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if err != nil {
		// Код 23505 — это нарушение уникальности (арендатор с таким именем уже существует)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert tenant: %w", err)
	}

	return &t, nil
}

// GetTenant возвращает арендатора по его ID.
func (r *PostgresRepo) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	query := `SELECT id, name, created_at FROM tenants WHERE id = $1`

	var t Tenant
	err := r.pool.QueryRow(ctx, query, id).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get tenant: %w", err)
	}

	return &t, nil
}
