package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepo реализует интерфейс Repository для PostgreSQL.
type PostgresRepo struct {
	pool *pgxpool.Pool
}

// NewPostgresRepo создает пул соединений с базой данных и проверяет доступность.
func NewPostgresRepo(ctx context.Context, connString string) (*PostgresRepo, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Настройки пула соединений (ограничиваем ресурсы, чтобы не положить БД)
	config.MaxConns = 20
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Проверяем, что база действительно доступна
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &PostgresRepo{pool: pool}, nil
}

// Close закрывает пул соединений при завершении работы приложения.
func (r *PostgresRepo) Close() {
	r.pool.Close()
}

// GetPool возвращает пул соединений (нужен для тестов и вспомогательных операций).
func (r *PostgresRepo) GetPool() *pgxpool.Pool {
	return r.pool
}
