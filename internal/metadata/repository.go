package metadata

import (
	"context"
	"errors"
	"time"
)

// Бизнес-ошибки, которые могут возвращать методы репозитория.
// Хендлеры будут проверять их через errors.Is().
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrLeaseTaken    = errors.New("lease already taken")
)

// Tenant — арендатор (клиент).
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Bucket — бакет (контейнер для объектов), принадлежащий арендатору.
type Bucket struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Object — метаданные файла (без самих данных).
type Object struct {
	ID              string    `json:"id"`
	BucketID        string    `json:"bucket_id"`
	Key             string    `json:"key"`
	Size            int64     `json:"size"`
	ETag            string    `json:"etag"`
	ContentType     *string   `json:"content_type"`      // Указатель, может быть NULL
	EncryptionKeyID *string   `json:"encryption_key_id"` // Указатель, может быть NULL
	CreatedAt       time.Time `json:"created_at"`
}

// AuditLog — запись аудита (append-only).
type AuditLog struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Result    string    `json:"result"`
	CreatedAt time.Time `json:"created_at"`
}

// Lease — блокировка (fencing).
type Lease struct {
	ID        string
	Holder    string
	ExpiresAt time.Time
}

// Repository — интерфейс для работы с метаданными в базе данных.
// Все методы принимают context.Context для отмены операций и таймаутов.
type Repository interface {
	// --- Tenant ---
	CreateTenant(ctx context.Context, name string) (*Tenant, error)
	GetTenant(ctx context.Context, id string) (*Tenant, error)

	// --- Bucket ---
	CreateBucket(ctx context.Context, tenantID, name string) (*Bucket, error)
	GetBucket(ctx context.Context, tenantID, name string) (*Bucket, error)
	ListBuckets(ctx context.Context, tenantID string) ([]*Bucket, error)
	DeleteBucket(ctx context.Context, tenantID, name string) error

	// --- Object ---
	PutObject(ctx context.Context, bucketID, key string, size int64, etag string, contentType string, encryptionKeyID string) (*Object, error)
	GetObject(ctx context.Context, bucketID, key string) (*Object, error)
	DeleteObject(ctx context.Context, bucketID, key string) error
	ListObjects(ctx context.Context, bucketID string) ([]*Object, error)

	// --- Audit ---
	WriteAudit(ctx context.Context, log *AuditLog) error

	// --- Job queue ---
	Enqueue(ctx context.Context, jobType string, payload []byte) (int64, error)
	Dequeue(ctx context.Context, workerID string) (*Job, error)
	CompleteJob(ctx context.Context, jobID int64) error
	FailJob(ctx context.Context, jobID int64, errMsg string, maxAttempts int) error

	// --- Lease (fencing) ---
	AcquireLease(ctx context.Context, leaseID, holder string, ttl time.Duration) error
	ReleaseLease(ctx context.Context, leaseID, holder string) error
}
