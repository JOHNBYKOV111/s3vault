package blob

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReplicaStore — хранилище, которое пишет данные в две директории (две "ноды").
// При чтении берет данные из первой доступной копии.
type ReplicaStore struct {
	dirs []string
}

// NewReplicaStore создает реплицированное хранилище.
func NewReplicaStore(dirs ...string) *ReplicaStore {
	return &ReplicaStore{dirs: dirs}
}

// Put записывает данные во все директории.
func (s *ReplicaStore) Put(bucket, key string, r io.Reader) (int64, error) {
	// Читаем все данные в память (для простоты; в реальном коде нужно стримить).
	// В будущем здесь будет стриминг через io.TeeReader в несколько директорий.
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("read data: %w", err)
	}

	for _, dir := range s.dirs {
		bucketDir := filepath.Join(dir, bucket)
		if err := os.MkdirAll(bucketDir, 0755); err != nil {
			return 0, fmt.Errorf("create bucket dir: %w", err)
		}

		filePath := filepath.Join(bucketDir, key)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return 0, fmt.Errorf("create key dir: %w", err)
		}

		file, err := os.Create(filePath)
		if err != nil {
			return 0, fmt.Errorf("create file: %w", err)
		}

		if _, err := file.Write(data); err != nil {
			if closeErr := file.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "blob: close after write error: %v\n", closeErr)
			}
			return 0, fmt.Errorf("write data: %w", err)
		}
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "blob: close file %s: %v\n", filePath, err)
		}
	}

	return int64(len(data)), nil
}

// Get читает данные из первой доступной директории.
func (s *ReplicaStore) Get(bucket, key string) (io.ReadCloser, error) {
	var lastErr error

	for _, dir := range s.dirs {
		filePath := filepath.Join(dir, bucket, key)
		file, err := os.Open(filePath)
		if err == nil {
			return file, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// List возвращает список ключей из первой директории (предполагая, что они синхронизированы).
func (s *ReplicaStore) List(bucket string) ([]string, error) {
	if len(s.dirs) == 0 {
		return nil, fmt.Errorf("no directories configured")
	}

	bucketDir := filepath.Join(s.dirs[0], bucket)
	var keys []string

	err := filepath.Walk(bucketDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(bucketDir, path)
			if err == nil {
				keys = append(keys, rel)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	return keys, nil
}

// Stat возвращает информацию о файле из первой доступной директории.
func (s *ReplicaStore) Stat(bucket, key string) (os.FileInfo, error) {
	var lastErr error

	for _, dir := range s.dirs {
		filePath := filepath.Join(dir, bucket, key)
		info, err := os.Stat(filePath)
		if err == nil {
			return info, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// Heal восстанавливает файл в директориях, где его нет.
// Ищет файл в одной из директорий и копирует его в остальные.
func (s *ReplicaStore) Heal(bucket, key string) error {
	var sourceDir string
	var sourcePath string

	// 1. Ищем файл хотя бы в одной директории
	for _, dir := range s.dirs {
		path := filepath.Join(dir, bucket, key)
		if _, err := os.Stat(path); err == nil {
			sourceDir = dir
			sourcePath = path
			break
		}
	}

	if sourcePath == "" {
		return fmt.Errorf("file not found in any directory: %s/%s", bucket, key)
	}

	// 2. Читаем данные из найденной директории
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}

	// 3. Копируем файл во все остальные директории, где его нет
	for _, dir := range s.dirs {
		if dir == sourceDir {
			continue
		}
		path := filepath.Join(dir, bucket, key)
		if _, err := os.Stat(path); err == nil {
			continue // файл уже есть
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("create dir: %w", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("write healed file: %w", err)
		}
	}

	return nil
}

// Delete удаляет файл из всех директорий. Idempotent: если файла нет — возвращает nil.
func (s *ReplicaStore) Delete(bucket, key string) error {
	var lastErr error

	for _, dir := range s.dirs {
		filePath := filepath.Join(dir, bucket, key)
		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				continue // idempotent: файла не было в этой реплике
			}
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("failed to delete file: %w", lastErr)
	}
	return nil
}
