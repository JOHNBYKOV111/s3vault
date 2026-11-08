package s3api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/metadata"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupMultipartTest(t *testing.T) (*ObjectHandler, *metadata.PostgresRepo, func()) {
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
		CREATE TABLE tenants (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE buckets (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(tenant_id, name));
		CREATE TABLE objects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE, key TEXT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, content_type TEXT, encryption_key_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(bucket_id, key));
		CREATE TABLE multipart_uploads (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE, key TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE parts (id BIGSERIAL PRIMARY KEY, upload_id UUID NOT NULL REFERENCES multipart_uploads(id) ON DELETE CASCADE, part_number INT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(upload_id, part_number));
	`
	if _, err := repo.GetPool().Exec(ctx, migration); err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migration: %v", err)
	}

	fsStore, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to create fs store: %v", err)
	}
	handler := NewObjectHandler(fsStore, repo)

	// Создаем директорию для multipart (части пишутся туда)
	if err := os.MkdirAll("./multipart", 0755); err != nil {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to create multipart dir: %v", err)
	}

	cleanup := func() {
		repo.Close()
		_ = pgContainer.Terminate(ctx)
		_ = os.RemoveAll("./multipart")
	}

	return handler, repo, cleanup
}

func TestMultipartUpload(t *testing.T) {
	handler, repo, cleanup := setupMultipartTest(t)
	defer cleanup()

	ctx := context.Background()
	bucket := "test-bucket"
	key := "large-file.bin"

	// Создаем тенанта и бакет
	tenantID := "00000000-0000-0000-0000-000000000001"
	_, err := repo.GetPool().Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, 'test-tenant')", tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	_, err = repo.GetPool().Exec(ctx, "INSERT INTO buckets (tenant_id, name) VALUES ($1, $2)", tenantID, bucket)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// 1. CreateMultipartUpload
	req := httptest.NewRequest("POST", "/"+bucket+"/"+key+"?uploads", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("CreateMultipartUpload failed: %d", rr.Code)
	}
	uploadID := rr.Header().Get("X-Amz-Upload-Id")
	if uploadID == "" {
		t.Fatal("expected UploadId in response")
	}

	// 2. UploadPart (часть 1)
	part1Data := []byte("Hello, ")
	req = httptest.NewRequest("PUT", "/"+bucket+"/"+key+"?partNumber=1&uploadId="+uploadID, bytes.NewReader(part1Data))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UploadPart 1 failed: %d", rr.Code)
	}
	etag1 := rr.Header().Get("ETag")

	// 3. UploadPart (часть 2)
	part2Data := []byte("Multipart World!")
	req = httptest.NewRequest("PUT", "/"+bucket+"/"+key+"?partNumber=2&uploadId="+uploadID, bytes.NewReader(part2Data))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UploadPart 2 failed: %d", rr.Code)
	}
	etag2 := rr.Header().Get("ETag")

	// 4. CompleteMultipartUpload
	completeXML := fmt.Sprintf(`<CompleteMultipartUpload>
		<Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part>
		<Part><PartNumber>2</PartNumber><ETag>%s</ETag></Part>
	</CompleteMultipartUpload>`, etag1, etag2)

	req = httptest.NewRequest("POST", "/"+bucket+"/"+key+"?uploadId="+uploadID, bytes.NewReader([]byte(completeXML)))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("CompleteMultipartUpload failed: %d", rr.Code)
	}

	// 5. Проверяем, что объект появился в базе
	var size int64
	err = repo.GetPool().QueryRow(ctx, "SELECT size FROM objects WHERE key = $1", key).Scan(&size)
	if err != nil {
		t.Fatalf("object not found in db: %v", err)
	}
	expectedSize := int64(len(part1Data) + len(part2Data))
	if size != expectedSize {
		t.Errorf("expected size %d, got %d", expectedSize, size)
	}

	// 6. Проверяем, что upload удален
	var uploadCount int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM multipart_uploads WHERE id = $1", uploadID).Scan(&uploadCount)
	if err != nil {
		t.Fatalf("failed to query uploads: %v", err)
	}
	if uploadCount != 0 {
		t.Errorf("expected 0 uploads after complete, got %d", uploadCount)
	}
}

func TestMultipartAbort(t *testing.T) {
	handler, repo, cleanup := setupMultipartTest(t)
	defer cleanup()

	ctx := context.Background()
	bucket := "test-bucket"
	key := "aborted-file.bin"

	// Создаем тенанта и бакет
	tenantID := "00000000-0000-0000-0000-000000000002"
	_, err := repo.GetPool().Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, 'test-tenant-abort')", tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	_, err = repo.GetPool().Exec(ctx, "INSERT INTO buckets (tenant_id, name) VALUES ($1, $2)", tenantID, bucket)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// 1. CreateMultipartUpload
	req := httptest.NewRequest("POST", "/"+bucket+"/"+key+"?uploads", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("CreateMultipartUpload failed: %d", rr.Code)
	}
	uploadID := rr.Header().Get("X-Amz-Upload-Id")

	// 2. UploadPart
	partData := []byte("This will be aborted")
	req = httptest.NewRequest("PUT", "/"+bucket+"/"+key+"?partNumber=1&uploadId="+uploadID, bytes.NewReader(partData))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("UploadPart failed: %d", rr.Code)
	}

	// 3. AbortMultipartUpload
	req = httptest.NewRequest("DELETE", "/"+bucket+"/"+key+"?uploadId="+uploadID, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("AbortMultipartUpload failed: %d", rr.Code)
	}

	// 4. Проверяем, что upload удален
	var uploadCount int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM multipart_uploads WHERE id = $1", uploadID).Scan(&uploadCount)
	if err != nil {
		t.Fatalf("failed to query uploads: %v", err)
	}
	if uploadCount != 0 {
		t.Errorf("expected 0 uploads after abort, got %d", uploadCount)
	}

	// 5. Проверяем, что объект не создался
	var objCount int
	err = repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM objects WHERE key = $1", key).Scan(&objCount)
	if err != nil {
		t.Fatalf("failed to query objects: %v", err)
	}
	if objCount != 0 {
		t.Errorf("expected 0 objects after abort, got %d", objCount)
	}
}

func TestUploadPartNonExistentUpload(t *testing.T) {
	handler, repo, cleanup := setupMultipartTest(t)
	defer cleanup()

	ctx := context.Background()
	bucket := "test-bucket"
	key := "nonexistent-upload.bin"
	uploadID := "00000000-0000-0000-0000-000000000000"

	// Создаем тенанта и бакет
	tenantID := "00000000-0000-0000-0000-000000000003"
	_, err := repo.GetPool().Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, 'test-tenant-404')", tenantID)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	_, err = repo.GetPool().Exec(ctx, "INSERT INTO buckets (tenant_id, name) VALUES ($1, $2)", tenantID, bucket)
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// Пытаемся загрузить часть в несуществующий upload
	partData := []byte("Hello")
	req := httptest.NewRequest("PUT", "/"+bucket+"/"+key+"?partNumber=1&uploadId="+uploadID, bytes.NewReader(partData))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Ожидаем 404 (NoSuchUpload)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent upload, got %d", rr.Code)
	}
}
