package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PutObject сохраняет метаданные объекта в базу.
// Если объект с таким ключом уже существует в бакете, он обновляется (upsert).
func (r *PostgresRepo) PutObject(ctx context.Context, bucketID, key string, size int64, etag string, contentType string, encryptionKeyID string) (*Object, error) {
	query := `
		INSERT INTO objects (bucket_id, key, size, etag, content_type, encryption_key_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bucket_id, key) DO UPDATE 
		SET size = EXCLUDED.size, etag = EXCLUDED.etag, content_type = EXCLUDED.content_type, encryption_key_id = EXCLUDED.encryption_key_id
		RETURNING id, bucket_id, key, size, etag, content_type, encryption_key_id, created_at
	`

	var o Object
	err := r.pool.QueryRow(ctx, query, bucketID, key, size, etag, contentType, encryptionKeyID).Scan(
		&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ETag, &o.ContentType, &o.EncryptionKeyID, &o.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("put object: %w", err)
	}

	return &o, nil
}

// GetObject возвращает метаданные объекта по ID бакета и ключу.
// Если объект не найден, возвращает ErrNotFound.
func (r *PostgresRepo) GetObject(ctx context.Context, bucketID, key string) (*Object, error) {
	query := `
		SELECT id, bucket_id, key, size, etag, content_type, encryption_key_id, created_at
		FROM objects
		WHERE bucket_id = $1 AND key = $2
	`

	var o Object
	err := r.pool.QueryRow(ctx, query, bucketID, key).Scan(
		&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ETag, &o.ContentType, &o.EncryptionKeyID, &o.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get object: %w", err)
	}

	return &o, nil
}

// DeleteObject удаляет метаданные объекта.
// Если объекта не было, возвращает ErrNotFound.
func (r *PostgresRepo) DeleteObject(ctx context.Context, bucketID, key string) error {
	query := `DELETE FROM objects WHERE bucket_id = $1 AND key = $2`

	tag, err := r.pool.Exec(ctx, query, bucketID, key)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ListObjects возвращает список всех объектов в бакете, отсортированный по ключу.
func (r *PostgresRepo) ListObjects(ctx context.Context, bucketID string) ([]*Object, error) {
	query := `
		SELECT id, bucket_id, key, size, etag, content_type, encryption_key_id, created_at
		FROM objects
		WHERE bucket_id = $1
		ORDER BY key
	`

	rows, err := r.pool.Query(ctx, query, bucketID)
	if err != nil {
		return nil, fmt.Errorf("query objects: %w", err)
	}
	defer rows.Close()

	var objects []*Object
	for rows.Next() {
		var o Object
		if err := rows.Scan(&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ETag, &o.ContentType, &o.EncryptionKeyID, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan object: %w", err)
		}
		objects = append(objects, &o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return objects, nil
}
