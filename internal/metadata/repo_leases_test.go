package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLeases(t *testing.T) {
	connString := "postgres://s3vault:s3vault@localhost:5433/s3vault?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	repo := &PostgresRepo{pool: pool}

	leaseID := "test-lease-" + time.Now().Format("150405.000000")
	holder1 := "worker-1"
	holder2 := "worker-2"

	// 1. Первый воркер берет блокировку
	err = repo.AcquireLease(ctx, leaseID, holder1, 1*time.Second)
	if err != nil {
		t.Fatalf("holder1 failed to acquire lease: %v", err)
	}

	// 2. Второй воркер пытается взять ту же блокировку (должен получить ErrLeaseTaken)
	err = repo.AcquireLease(ctx, leaseID, holder2, 1*time.Second)
	if err != ErrLeaseTaken {
		t.Errorf("expected ErrLeaseTaken, got %v", err)
	}

	// 3. Ждем, пока истечет TTL (1 секунда)
	time.Sleep(1500 * time.Millisecond)

	// 4. Теперь второй воркер может взять блокировку
	err = repo.AcquireLease(ctx, leaseID, holder2, 1*time.Second)
	if err != nil {
		t.Fatalf("holder2 failed to acquire expired lease: %v", err)
	}

	// 5. Освобождаем блокировку
	err = repo.ReleaseLease(ctx, leaseID, holder2)
	if err != nil {
		t.Fatalf("failed to release lease: %v", err)
	}

	// 6. Проверяем, что блокировка больше не существует
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM leases WHERE id = $1", leaseID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query leases: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 leases after release, got %d", count)
	}
}
