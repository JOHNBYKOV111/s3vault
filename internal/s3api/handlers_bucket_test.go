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

// setupTestRepo создает реальный репозиторий для теста.
func setupTestRepo(t *testing.T) metadata.Repository {
	t.Helper()

	connString := "postgres://s3vault:s3vault@localhost:5433/s3vault?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	repo, err := metadata.NewPostgresRepo(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}
	return repo
}

func TestHandleCreateBucket(t *testing.T) {
	repo := setupTestRepo(t)
	defer repo.(*metadata.PostgresRepo).Close()

	store := &mockStore{}
	handler := NewObjectHandler(store, repo)

	// Уникальное имя бакета, чтобы не конфликтовать
	bucketName := fmt.Sprintf("test-handler-bucket-%d", time.Now().UnixNano())

	req := httptest.NewRequest(http.MethodPut, "/"+bucketName, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// В тесте используется дефолтный tenant_id (захардкожен в хендлере)
	tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
	bucket, err := repo.GetBucket(context.Background(), tenantID, bucketName)
	if err != nil {
		t.Fatalf("bucket not found in db: %v", err)
	}
	if bucket.Name != bucketName {
		t.Errorf("expected bucket name '%s', got '%s'", bucketName, bucket.Name)
	}

	// Убираем за собой
	repo.DeleteBucket(context.Background(), tenantID, bucketName)
}

func TestHandleListObjectsV2_FromDB(t *testing.T) {
	repo := setupTestRepo(t)
	defer repo.(*metadata.PostgresRepo).Close()

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
