package metadata

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestObjectIsolation(t *testing.T) {
	repo, cleanup := SetupTestRepoWithContainer(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Создаем двух арендаторов с уникальными именами
	tenantAName := fmt.Sprintf("tenant-a-isolation-%d", time.Now().UnixNano())
	tenantA, err := repo.CreateTenant(ctx, tenantAName)
	if err != nil {
		t.Fatalf("failed to create tenant A: %v", err)
	}
	defer func() {
		if _, err := repo.GetPool().Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA.ID); err != nil {
			t.Logf("cleanup tenant A: %v", err)
		}
	}()

	tenantBName := fmt.Sprintf("tenant-b-isolation-%d", time.Now().UnixNano())
	tenantB, err := repo.CreateTenant(ctx, tenantBName)
	if err != nil {
		t.Fatalf("failed to create tenant B: %v", err)
	}
	defer func() {
		if _, err := repo.GetPool().Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB.ID); err != nil {
			t.Logf("cleanup tenant B: %v", err)
		}
	}()

	// 2. Создаем бакеты для каждого
	bucketA, err := repo.CreateBucket(ctx, tenantA.ID, "bucket-a")
	if err != nil {
		t.Fatalf("failed to create bucket A: %v", err)
	}
	bucketB, err := repo.CreateBucket(ctx, tenantB.ID, "bucket-b")
	if err != nil {
		t.Fatalf("failed to create bucket B: %v", err)
	}

	// 3. Кладем объект в бакет A (добавили "" в качестве encryptionKeyID)
	_, err = repo.PutObject(ctx, bucketA.ID, "secret.txt", 100, "etag-a", "text/plain", "")
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// 4. Пытаемся получить этот объект из бакета B (должно быть ErrNotFound)
	_, err = repo.GetObject(ctx, bucketB.ID, "secret.txt")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for tenant B, got %v", err)
	}

	// 5. Проверяем, что из бакета A объект виден
	obj, err := repo.GetObject(ctx, bucketA.ID, "secret.txt")
	if err != nil {
		t.Fatalf("expected object in bucket A, got error: %v", err)
	}
	if obj.Key != "secret.txt" {
		t.Errorf("expected key 'secret.txt', got '%s'", obj.Key)
	}

	// Убираем за собой (cleanup контейнера удалит всё, но проверим ошибки)
	if _, err := repo.GetPool().Exec(ctx, "DELETE FROM objects WHERE bucket_id = $1", bucketA.ID); err != nil {
		t.Logf("cleanup objects: %v", err)
	}
	if _, err := repo.GetPool().Exec(ctx, "DELETE FROM buckets WHERE id = $1", bucketA.ID); err != nil {
		t.Logf("cleanup bucket A: %v", err)
	}
	if _, err := repo.GetPool().Exec(ctx, "DELETE FROM buckets WHERE id = $1", bucketB.ID); err != nil {
		t.Logf("cleanup bucket B: %v", err)
	}
}
