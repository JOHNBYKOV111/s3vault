package blob

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/metadata"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupDedupTest(t *testing.T) (*DedupStore, *metadata.PostgresRepo, func()) {
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

	repo, err := metadata.NewPostgresRepo(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to create repo: %v", err)
	}

	migration := `
		CREATE TABLE blobs (id TEXT PRIMARY KEY, size BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE objects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL, key TEXT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, content_type TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE blob_refs (id BIGSERIAL PRIMARY KEY, object_id UUID NOT NULL REFERENCES objects(id) ON DELETE CASCADE, blob_id TEXT NOT NULL REFERENCES blobs(id) ON DELETE RESTRICT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
	`
	if _, err := repo.GetPool().Exec(ctx, migration); err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}

	fsStore, err := NewFSStore(t.TempDir())
	if err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to create fs store: %v", err)
	}
	dedupStore := NewDedupStore(fsStore, repo)

	cleanup := func() {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
	}

	return dedupStore, repo, cleanup
}

func TestDedupStore(t *testing.T) {
	store, repo, cleanup := setupDedupTest(t)
	defer cleanup()

	ctx := context.Background()
	bucket := "test-bucket"
	key1 := "file1.txt"
	key2 := "file2.txt"
	data := []byte("Hello, Deduplicated S3!")

	// Создаем объекты в базе (нужны для внешних ключей в blob_refs)
	// Используем прямой SQL, так как repo.PutObject требует bucket_id (UUID).
	bucketID := "00000000-0000-0000-0000-000000000001"
	_, err := repo.GetPool().Exec(ctx, "INSERT INTO objects (id, bucket_id, key, size, etag) VALUES (gen_random_uuid(), $1, $2, $3, 'etag1')", bucketID, key1, len(data))
	if err != nil {
		t.Fatalf("failed to insert object1: %v", err)
	}
	var obj1ID string
	err = repo.GetPool().QueryRow(ctx, "SELECT id FROM objects WHERE key = $1", key1).Scan(&obj1ID)
	if err != nil {
		t.Fatalf("failed to get object1 id: %v", err)
	}

	_, err = repo.GetPool().Exec(ctx, "INSERT INTO objects (id, bucket_id, key, size, etag) VALUES (gen_random_uuid(), $1, $2, $3, 'etag2')", bucketID, key2, len(data))
	if err != nil {
		t.Fatalf("failed to insert object2: %v", err)
	}
	var obj2ID string
	err = repo.GetPool().QueryRow(ctx, "SELECT id FROM objects WHERE key = $1", key2).Scan(&obj2ID)
	if err != nil {
		t.Fatalf("failed to get object2 id: %v", err)
	}

	// 1. Записываем один и тот же файл дважды
	n1, err := store.Put(bucket, key1, bytes.NewReader(data), obj1ID)
	if err != nil {
		t.Fatalf("failed to put file1: %v", err)
	}
	n2, err := store.Put(bucket, key2, bytes.NewReader(data), obj2ID)
	if err != nil {
		t.Fatalf("failed to put file2: %v", err)
	}
	if n1 != n2 {
		t.Errorf("expected same size, got %d and %d", n1, n2)
	}

	// 2. Проверяем, что blob один, а ссылок две
	var blobCount, refCount int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blobs").Scan(&blobCount)
	if err != nil {
		t.Fatalf("failed to query blobs: %v", err)
	}
	if blobCount != 1 {
		t.Errorf("expected 1 blob, got %d", blobCount)
	}

	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blob_refs").Scan(&refCount)
	if err != nil {
		t.Fatalf("failed to query blob_refs: %v", err)
	}
	if refCount != 2 {
		t.Errorf("expected 2 refs, got %d", refCount)
	}

	// 3. Читаем данные (должны совпадать)
	reader, err := store.Get(bucket, key1)
	if err != nil {
		t.Fatalf("failed to get file1: %v", err)
	}
	readData, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("failed to read file1: %v", err)
	}
	if !bytes.Equal(data, readData) {
		t.Errorf("expected %s, got %s", data, readData)
	}

	// 4. Удаляем первый объект (ссылка должна уменьшиться)
	err = store.Delete(bucket, key1, obj1ID)
	if err != nil {
		t.Fatalf("failed to delete file1: %v", err)
	}

	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blob_refs").Scan(&refCount)
	if err != nil {
		t.Fatalf("failed to query blob_refs after delete1: %v", err)
	}
	if refCount != 1 {
		t.Errorf("expected 1 ref after delete1, got %d", refCount)
	}

	// 5. Удаляем второй объект (blob должен удалиться)
	err = store.Delete(bucket, key2, obj2ID)
	if err != nil {
		t.Fatalf("failed to delete file2: %v", err)
	}

	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blobs").Scan(&blobCount)
	if err != nil {
		t.Fatalf("failed to query blobs after delete2: %v", err)
	}
	if blobCount != 0 {
		t.Errorf("expected 0 blobs after delete2, got %d", blobCount)
	}

	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blob_refs").Scan(&refCount)
	if err != nil {
		t.Fatalf("failed to query blob_refs after delete2: %v", err)
	}
	if refCount != 0 {
		t.Errorf("expected 0 refs after delete2, got %d", refCount)
	}
}
