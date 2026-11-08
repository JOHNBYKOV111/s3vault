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

func setupTestRepoWithContainer(t *testing.T) (*PostgresRepo, func()) {
	t.Helper()
	ctx := context.Background()

	// 1. Запускаем контейнер с Postgres
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

	// 2. Получаем строку подключения
	connString, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	// 3. Применяем миграцию (создаем таблицы)
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to connect to db: %v", err)
	}

	migration := `
		CREATE TABLE tenants (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE buckets (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(tenant_id, name));
		CREATE TABLE objects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE, key TEXT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, content_type TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(bucket_id, key));
		CREATE TABLE audit_log (id BIGSERIAL PRIMARY KEY, tenant_id UUID, action TEXT NOT NULL, resource TEXT NOT NULL, result TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE blobs (id TEXT PRIMARY KEY, size BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE blob_refs (id BIGSERIAL PRIMARY KEY, object_id UUID NOT NULL REFERENCES objects(id) ON DELETE CASCADE, blob_id TEXT NOT NULL REFERENCES blobs(id) ON DELETE RESTRICT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE jobs (id BIGSERIAL PRIMARY KEY, type TEXT NOT NULL, payload JSONB NOT NULL, status TEXT NOT NULL DEFAULT 'pending', attempts INT NOT NULL DEFAULT 0, last_error TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE leases (id TEXT PRIMARY KEY, holder TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL);
	`
	if _, err := pool.Exec(ctx, migration); err != nil {
		pool.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}
	pool.Close()

	// 4. Создаем репозиторий с этим подключением
	pool, err = pgxpool.New(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to connect to db: %v", err)
	}
	repo := &PostgresRepo{pool: pool}

	// 5. Возвращаем функцию для очистки
	cleanup := func() {
		pool.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	}

	return repo, cleanup
}

func TestBucketRepository(t *testing.T) {
	repo, cleanup := setupTestRepoWithContainer(t)
	defer cleanup()

	ctx := context.Background()

	// Создаем временного арендатора
	var tenantID string
	err := repo.pool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ('test-tenant') RETURNING id").Scan(&tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	bucketName := "test-bucket"

	// 1. Успешное создание бакета
	t.Run("CreateBucket", func(t *testing.T) {
		bucket, err := repo.CreateBucket(ctx, tenantID, bucketName)
		if err != nil {
			t.Fatalf("failed to create bucket: %v", err)
		}
		if bucket.Name != bucketName {
			t.Errorf("expected name %s, got %s", bucketName, bucket.Name)
		}
		if bucket.TenantID != tenantID {
			t.Errorf("expected tenant_id %s, got %s", tenantID, bucket.TenantID)
		}
		if bucket.ID == "" {
			t.Error("expected non-empty bucket ID")
		}
	})

	// 2. Попытка создать дубликат (должна вернуть ErrAlreadyExists)
	t.Run("CreateDuplicate", func(t *testing.T) {
		_, err := repo.CreateBucket(ctx, tenantID, bucketName)
		if err != ErrAlreadyExists {
			t.Errorf("expected ErrAlreadyExists, got %v", err)
		}
	})

	// 3. Получение существующего бакета
	t.Run("GetBucket", func(t *testing.T) {
		bucket, err := repo.GetBucket(ctx, tenantID, bucketName)
		if err != nil {
			t.Fatalf("failed to get bucket: %v", err)
		}
		if bucket.Name != bucketName {
			t.Errorf("expected name %s, got %s", bucketName, bucket.Name)
		}
	})

	// 4. Получение несуществующего бакета (должно вернуть ErrNotFound)
	t.Run("GetNonExistent", func(t *testing.T) {
		_, err := repo.GetBucket(ctx, tenantID, "non-existent-bucket")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	// 5. Список бакетов (должен быть ровно 1)
	t.Run("ListBuckets", func(t *testing.T) {
		buckets, err := repo.ListBuckets(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to list buckets: %v", err)
		}
		if len(buckets) != 1 {
			t.Errorf("expected 1 bucket, got %d", len(buckets))
		}
		if buckets[0].Name != bucketName {
			t.Errorf("expected name %s, got %s", bucketName, buckets[0].Name)
		}
	})

	// 6. Удаление бакета
	t.Run("DeleteBucket", func(t *testing.T) {
		err := repo.DeleteBucket(ctx, tenantID, bucketName)
		if err != nil {
			t.Fatalf("failed to delete bucket: %v", err)
		}

		// Проверяем, что его больше нет
		_, err = repo.GetBucket(ctx, tenantID, bucketName)
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
	})

	// 7. Удаление несуществующего бакета (должно вернуть ErrNotFound)
	t.Run("DeleteNonExistent", func(t *testing.T) {
		err := repo.DeleteBucket(ctx, tenantID, "non-existent-bucket")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}
