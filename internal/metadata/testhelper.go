package metadata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SetupTestRepoWithContainer поднимает Postgres через testcontainers,
// применяет миграции из migrations/000001_init.up.sql и возвращает
// *PostgresRepo + cleanup. Используется в тестах пакетов metadata и s3api.
func SetupTestRepoWithContainer(t *testing.T) (*PostgresRepo, func()) {
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
				WithStartupTimeout(120*time.Second),
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

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to connect to db: %v", err)
	}

	// Ищем миграцию относительно файла testhelper.go (а не текущей директории).
	// Это работает и для metadata, и для s3api (где migrations/ нет рядом).
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(filepath.Dir(thisFile), "migrations", "000001_init.up.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		pool.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to read migration %s: %v", migrationPath, err)
	}

	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		pool.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}

	repo := &PostgresRepo{pool: pool}

	cleanup := func() {
		pool.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	}

	return repo, cleanup
}
