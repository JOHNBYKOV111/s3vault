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

// SetupTestRepoWithContainer поднимает Postgres через testcontainers,
// применяет миграции и возвращает *PostgresRepo + cleanup.
// Используется в тестах пакетов metadata и s3api.
func SetupTestRepoWithContainer(t *testing.T) (*PostgresRepo, func()) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("s3vault"),
		postgres.WithUsername("s3vault"),
		postgres.WithPassword("s3vault"),
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

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		t.Fatalf("failed to connect to db: %v", err)
	}

	migration := `
		CREATE TABLE tenants (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE buckets (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(tenant_id, name));
		CREATE TABLE objects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE, key TEXT NOT NULL, size BIGINT NOT NULL, etag TEXT NOT NULL, content_type TEXT, encryption_key_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(bucket_id, key));
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

	repo := &PostgresRepo{pool: pool}

	cleanup := func() {
		pool.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	}

	return repo, cleanup
}
