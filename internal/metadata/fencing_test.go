package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupFencingTest(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("s3vault"),
		postgres.WithUsername("s3vault"),
		postgres.WithPassword("s3vault"),
		// Ждём именно готовность БД принимать запросы (лог "ready to accept
		// connections"), а не просто открытый порт. Порт открывается раньше,
		// чем Postgres готов, и тогда ping падает с "the database system is
		// starting up". Таймаут — 120с, чтобы пережить холодный старт под
		// нагрузкой (несколько контейнеров параллельно в go test ./...).
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
		t.Fatalf("failed to connect: %v", err)
	}

	migration := `
		CREATE TABLE leases (
			id TEXT PRIMARY KEY,
			holder TEXT NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		);
	`
	if _, err := pool.Exec(ctx, migration); err != nil {
		pool.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}

	cleanup := func() {
		pool.Close()
		_ = pgContainer.Terminate(ctx)
	}

	return pool, cleanup
}

func TestFencing(t *testing.T) {
	pool, cleanup := setupFencingTest(t)
	defer cleanup()

	ctx := context.Background()
	repo := &PostgresRepo{pool: pool}

	leaseID := "test-lease"
	holder1 := "instance-1"
	holder2 := "instance-2"

	// Канал для синхронизации горутин
	result1 := make(chan error, 1)
	result2 := make(chan error, 1)

	// Горутина 1: пытается взять lease
	go func() {
		err := repo.AcquireLease(ctx, leaseID, holder1, 1*time.Second)
		result1 <- err
	}()

	// Горутина 2: пытается взять тот же lease
	go func() {
		err := repo.AcquireLease(ctx, leaseID, holder2, 1*time.Second)
		result2 <- err
	}()

	// Ждем результаты
	err1 := <-result1
	err2 := <-result2

	// Один должен получить nil (успех), другой — ErrLeaseTaken (503)
	if err1 == nil && err2 == nil {
		t.Error("expected one to fail (ErrLeaseTaken), but both succeeded")
	}
	if err1 != nil && err2 != nil {
		t.Error("expected one to succeed, but both failed")
	}

	// Проверяем, что один из них получил ErrLeaseTaken
	if err1 != nil && err1 != ErrLeaseTaken {
		t.Errorf("expected ErrLeaseTaken, got %v", err1)
	}
	if err2 != nil && err2 != ErrLeaseTaken {
		t.Errorf("expected ErrLeaseTaken, got %v", err2)
	}

	// Проверяем, что lease в базе принадлежит одному из holder'ов
	var holder string
	err := pool.QueryRow(ctx, "SELECT holder FROM leases WHERE id = $1", leaseID).Scan(&holder)
	if err != nil {
		t.Fatalf("failed to query lease: %v", err)
	}
	if holder != holder1 && holder != holder2 {
		t.Errorf("expected holder to be %s or %s, got %s", holder1, holder2, holder)
	}
}
