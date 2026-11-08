package metadata

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLeases(t *testing.T) {
	repo, cleanup := SetupTestRepoWithContainer(t)
	defer cleanup()

	ctx := context.Background()
	leaseID := fmt.Sprintf("test-lease-%d", time.Now().UnixNano())
	holder1 := "worker-1"
	holder2 := "worker-2"

	// 1. Первый воркер берет блокировку
	if err := repo.AcquireLease(ctx, leaseID, holder1, 1*time.Second); err != nil {
		t.Fatalf("holder1 failed to acquire lease: %v", err)
	}

	// 2. Второй воркер пытается взять ту же блокировку (должен получить ErrLeaseTaken)
	if err := repo.AcquireLease(ctx, leaseID, holder2, 1*time.Second); err != ErrLeaseTaken {
		t.Errorf("expected ErrLeaseTaken, got %v", err)
	}

	// 3. Ждем, пока истечет TTL (1 секунда)
	time.Sleep(1500 * time.Millisecond)

	// 4. Теперь второй воркер может взять блокировку
	if err := repo.AcquireLease(ctx, leaseID, holder2, 1*time.Second); err != nil {
		t.Fatalf("holder2 failed to acquire expired lease: %v", err)
	}

	// 5. Освобождаем блокировку
	if err := repo.ReleaseLease(ctx, leaseID, holder2); err != nil {
		t.Fatalf("failed to release lease: %v", err)
	}

	// 6. Проверяем, что блокировка больше не существует
	var count int
	if err := repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM leases WHERE id = $1", leaseID).Scan(&count); err != nil {
		t.Fatalf("failed to query leases: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 leases after release, got %d", count)
	}
}
