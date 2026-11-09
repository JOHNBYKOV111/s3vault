package s3api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/metadata"
)

func TestHandleCreateBucket(t *testing.T) {
	repo, cleanup := metadata.SetupTestRepoWithContainer(t)
	defer cleanup()

	// Реальный FSStore на temp dir (без mockStore).
	store, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create fs store: %v", err)
	}
	handler := NewObjectHandler(store, repo)

	ctx := context.Background()

	// 1. Создаем tenant (хендлер требует tenant_id для создания бакета)
	tenant, err := repo.CreateTenant(ctx, fmt.Sprintf("test-tenant-bucket-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// 2. Создаем бакет через HTTP-запрос
	bucketName := fmt.Sprintf("test-handler-bucket-%d", time.Now().UnixNano())
	req := httptest.NewRequest(http.MethodPut, "/"+bucketName, nil)
	// Передаем tenant_id в контекст через приватный ключ (contextKey),
	// чтобы избежать SA1029 (не использовать string как ключ).
	req = req.WithContext(context.WithValue(req.Context(), tenantIDKey, tenant.ID))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// 3. Проверяем, что бакет создан в БД.
	// Cleanup не нужен: testcontainers удалит контейнер целиком (вместе с БД).
	bucket, err := repo.GetBucket(ctx, tenant.ID, bucketName)
	if err != nil {
		t.Fatalf("bucket not found in db: %v", err)
	}
	if bucket.Name != bucketName {
		t.Errorf("expected bucket name '%s', got '%s'", bucketName, bucket.Name)
	}
}

func TestHandleListObjectsV2_FromDB(t *testing.T) {
	repo, cleanup := metadata.SetupTestRepoWithContainer(t)
	defer cleanup()

	// Реальный FSStore на temp dir (без mockStore).
	store, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create fs store: %v", err)
	}
	handler := NewObjectHandler(store, repo)

	ctx := context.Background()
	// Уникальное имя арендатора, чтобы не конфликтовать.
	tenantName := fmt.Sprintf("test-tenant-list-%d", time.Now().UnixNano())
	tenant, err := repo.CreateTenant(ctx, tenantName)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Cleanup не нужен: testcontainers удалит контейнер целиком (вместе с БД).
	bucket, err := repo.CreateBucket(ctx, tenant.ID, "test-bucket-list")
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// Добавили "" в качестве encryptionKeyID (6-й аргумент).
	_, err = repo.PutObject(ctx, bucket.ID, "file1.txt", 100, "etag1", "text/plain", "")
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test-bucket-list?list-type=2", nil)
	ctxWithTenant := context.WithValue(req.Context(), tenantIDKey, tenant.ID)
	req = req.WithContext(ctxWithTenant)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<Key>file1.txt</Key>") {
		t.Errorf("expected XML to contain '<Key>file1.txt</Key>', got: %s", body)
	}
	if !strings.Contains(body, "<Size>100</Size>") {
		t.Errorf("expected XML to contain '<Size>100</Size>', got: %s", body)
	}
}
