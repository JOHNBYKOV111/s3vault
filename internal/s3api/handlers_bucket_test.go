package s3api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

func TestHandleCreateBucket(t *testing.T) {
	repo, cleanup := metadata.SetupTestRepoWithContainer(t)
	defer cleanup()

	store := &mockStore{}
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
	// Передаем tenant_id в контекст, как это делает middleware
	req = req.WithContext(context.WithValue(req.Context(), "tenant_id", tenant.ID))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// 3. Проверяем, что бакет создан в БД
	bucket, err := repo.GetBucket(ctx, tenant.ID, bucketName)
	if err != nil {
		t.Fatalf("bucket not found in db: %v", err)
	}
	if bucket.Name != bucketName {
		t.Errorf("expected bucket name '%s', got '%s'", bucketName, bucket.Name)
	}

	// Убираем за собой (опционально, cleanup контейнера удалит всё)
	repo.DeleteBucket(ctx, tenant.ID, bucketName)
}

func TestHandleListObjectsV2_FromDB(t *testing.T) {
	repo, cleanup := metadata.SetupTestRepoWithContainer(t)
	defer cleanup()

	store := &mockStore{}
	handler := NewObjectHandler(store, repo)

	ctx := context.Background()
	// Уникальное имя арендатора, чтобы не конфликтовать
	tenantName := fmt.Sprintf("test-tenant-list-%d", time.Now().UnixNano())
	tenant, err := repo.CreateTenant(ctx, tenantName)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	t.Cleanup(func() {
		repo.DeleteBucket(context.Background(), tenant.ID, "test-bucket-list")
	})

	bucket, err := repo.CreateBucket(ctx, tenant.ID, "test-bucket-list")
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// Добавили "" в качестве encryptionKeyID (6-й аргумент)
	_, err = repo.PutObject(ctx, bucket.ID, "file1.txt", 100, "etag1", "text/plain", "")
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test-bucket-list?list-type=2", nil)
	ctxWithTenant := context.WithValue(req.Context(), "tenant_id", tenant.ID)
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

// mockStore — заглушка для blob.Store, чтобы не писать на диск в тестах.
type mockStore struct{}

func (m *mockStore) Put(bucket, key string, r io.Reader) (int64, error) { return 0, nil }
func (m *mockStore) Get(bucket, key string) (io.ReadCloser, error)      { return nil, nil }
func (m *mockStore) List(bucket string) ([]string, error)               { return nil, nil }
func (m *mockStore) Stat(bucket, key string) (os.FileInfo, error)       { return nil, nil }
