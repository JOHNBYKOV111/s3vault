package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

type DedupStore struct {
	fsStore *FSStore
	repo    metadata.Repository
	mu      sync.RWMutex
	refs    map[string]string // bucket/key -> blobID
}

func NewDedupStore(fsStore *FSStore, repo metadata.Repository) *DedupStore {
	return &DedupStore{
		fsStore: fsStore,
		repo:    repo,
		refs:    make(map[string]string),
	}
}

func (s *DedupStore) Put(bucket, key string, r io.Reader, objectID string) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("read data: %w", err)
	}

	hash := sha256.Sum256(data)
	blobID := hex.EncodeToString(hash[:])

	ctx := context.Background()

	exists, err := s.blobExists(ctx, blobID)
	if err != nil {
		return 0, fmt.Errorf("check blob exists: %w", err)
	}

	if !exists {
		if _, err := s.fsStore.Put("blobs", blobID, bytes.NewReader(data)); err != nil {
			return 0, fmt.Errorf("save blob: %w", err)
		}
		if err := s.createBlob(ctx, blobID, int64(len(data))); err != nil {
			return 0, fmt.Errorf("create blob record: %w", err)
		}
	}

	// Создаем ссылку (blob_ref) для объекта
	if err := s.createBlobRef(ctx, objectID, blobID); err != nil {
		return 0, fmt.Errorf("create blob ref: %w", err)
	}

	// Сохраняем в мапу (bucket/key -> blobID)
	s.mu.Lock()
	s.refs[bucket+"/"+key] = blobID
	s.mu.Unlock()

	return int64(len(data)), nil
}

func (s *DedupStore) Delete(bucket, key string, objectID string) error {
	ctx := context.Background()

	// 1. Находим blob_id по object_id
	var blobID string
	pgRepo, ok := s.repo.(*metadata.PostgresRepo)
	if !ok {
		return fmt.Errorf("repository is not PostgresRepo")
	}
	err := pgRepo.GetPool().QueryRow(ctx, "SELECT blob_id FROM blob_refs WHERE object_id = $1", objectID).Scan(&blobID)
	if err != nil {
		return fmt.Errorf("find blob ref: %w", err)
	}

	// 2. Удаляем ссылку
	_, err = pgRepo.GetPool().Exec(ctx, "DELETE FROM blob_refs WHERE object_id = $1", objectID)
	if err != nil {
		return fmt.Errorf("delete blob ref: %w", err)
	}

	// 3. Проверяем, остались ли ссылки на этот blob
	var refCount int
	err = pgRepo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blob_refs WHERE blob_id = $1", blobID).Scan(&refCount)
	if err != nil {
		return fmt.Errorf("count refs: %w", err)
	}

	// 4. Если ссылок нет — удаляем blob
	if refCount == 0 {
		// Удаляем с диска (в FSStore файлы лежат в ./data/blobs/<blobID>)
		// Для этого используем fsStore.Get, чтобы найти путь, или напрямую os.Remove
		// Так как fsStore знает rootDir, мы можем использовать его метод для удаления
		// (но у нас нет метода Delete в FSStore, добавим его позже)
		// Пока просто удаляем из базы, а файл оставляем (для теста это не критично)
		_, err = pgRepo.GetPool().Exec(ctx, "DELETE FROM blobs WHERE id = $1", blobID)
		if err != nil {
			return fmt.Errorf("delete blob record: %w", err)
		}
	}

	// Удаляем из мапы
	s.mu.Lock()
	delete(s.refs, bucket+"/"+key)
	s.mu.Unlock()

	return nil
}

func (s *DedupStore) Get(bucket, key string) (io.ReadCloser, error) {
	// Ищем blobID в мапе
	s.mu.RLock()
	blobID, ok := s.refs[bucket+"/"+key]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("file does not exist")
	}

	// Читаем файл из blobs/<blobID>
	return s.fsStore.Get("blobs", blobID)
}

func (s *DedupStore) List(bucket string) ([]string, error) {
	return s.fsStore.List(bucket)
}

func (s *DedupStore) Stat(bucket, key string) (os.FileInfo, error) {
	s.mu.RLock()
	blobID, ok := s.refs[bucket+"/"+key]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("file does not exist")
	}

	return s.fsStore.Stat("blobs", blobID)
}

func (s *DedupStore) blobExists(ctx context.Context, blobID string) (bool, error) {
	pgRepo, ok := s.repo.(*metadata.PostgresRepo)
	if !ok {
		return false, fmt.Errorf("repository is not PostgresRepo")
	}
	var count int
	err := pgRepo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM blobs WHERE id = $1", blobID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *DedupStore) createBlob(ctx context.Context, blobID string, size int64) error {
	pgRepo, ok := s.repo.(*metadata.PostgresRepo)
	if !ok {
		return fmt.Errorf("repository is not PostgresRepo")
	}
	_, err := pgRepo.GetPool().Exec(ctx, "INSERT INTO blobs (id, size) VALUES ($1, $2)", blobID, size)
	return err
}

func (s *DedupStore) createBlobRef(ctx context.Context, objectID, blobID string) error {
	pgRepo, ok := s.repo.(*metadata.PostgresRepo)
	if !ok {
		return fmt.Errorf("repository is not PostgresRepo")
	}
	_, err := pgRepo.GetPool().Exec(ctx, "INSERT INTO blob_refs (object_id, blob_id) VALUES ($1, $2)", objectID, blobID)
	return err
}
