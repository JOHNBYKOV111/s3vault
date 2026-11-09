package s3api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/metadata"
)

func TestHandlePut_SavesMetadata(t *testing.T) {
	// 1. Поднимаем временный Postgres через testcontainers.
	//    Тест не зависит от внешней БД, изолирован.
	repo, cleanup := metadata.SetupTestRepoWithContainer(t)
	defer cleanup()

	// 2. Создаем временное хранилище на диске (реальный FSStore, без моков).
	store, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	handler := NewObjectHandler(store, repo)

	// 3. Создаем тестового арендатора с уникальным именем (чтобы избежать конфликтов).
	dbCtx := context.Background()
	tenantName := fmt.Sprintf("test-tenant-put-%d", time.Now().UnixNano())
	tenant, err := repo.CreateTenant(dbCtx, tenantName)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Cleanup не нужен: testcontainers удалит контейнер целиком (вместе с БД).
	bucket, err := repo.CreateBucket(dbCtx, tenant.ID, "test-bucket-put")
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// 4. Отправляем PUT-запрос с телом.
	body := []byte("Hello, S3 Metadata!")
	req := httptest.NewRequest(http.MethodPut, "/test-bucket-put/hello.txt", bytes.NewReader(body))

	// Передаем tenant_id в контекст через приватный ключ (contextKey).
	ctxWithTenant := context.WithValue(req.Context(), tenantIDKey, tenant.ID)
	req = req.WithContext(ctxWithTenant)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// 5. Проверяем, что ответ 200 OK.
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// 6. Проверяем, что объект реально появился в базе.
	obj, err := repo.GetObject(dbCtx, bucket.ID, "hello.txt")
	if err != nil {
		t.Fatalf("object not found in db: %v", err)
	}
	if obj.Size != int64(len(body)) {
		t.Errorf("expected size %d, got %d", len(body), obj.Size)
	}
}
