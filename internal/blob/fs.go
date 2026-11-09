package blob

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type FSStore struct {
	rootDir string
}

// NewFSStore создает хранилище в указанной директории.
func NewFSStore(rootDir string) (*FSStore, error) {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root dir: %w", err)
	}
	return &FSStore{rootDir: rootDir}, nil
}

func (s *FSStore) Put(bucket, key string, r io.Reader) (int64, error) {
	// Создаем директорию для бакета
	bucketDir := filepath.Join(s.rootDir, bucket)
	if err := os.MkdirAll(bucketDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create bucket dir: %w", err)
	}

	// Создаем файл
	filePath := filepath.Join(bucketDir, key)
	// Убедимся, что директории для ключа (если есть слэши) существуют
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return 0, fmt.Errorf("failed to create key dir: %w", err)
	}

	// Открываем файл для записи
	file, err := os.Create(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Копируем данные из reader в файл (стриминг!)
	n, err := io.Copy(file, r)
	if err != nil {
		return n, fmt.Errorf("failed to write data: %w", err)
	}

	return n, nil
}

func (s *FSStore) Get(bucket, key string) (io.ReadCloser, error) {
	filePath := filepath.Join(s.rootDir, bucket, key)
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

// List рекурсивно обходит папку бакета и возвращает список ключей.
func (s *FSStore) List(bucket string) ([]string, error) {
	bucketDir := filepath.Join(s.rootDir, bucket)
	var keys []string

	err := filepath.Walk(bucketDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // Бакет пустой или не существует — возвращаем пустой список
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
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}
	return keys, nil
}

// Stat возвращает информацию о файле (размер, время изменения).
func (s *FSStore) Stat(bucket, key string) (os.FileInfo, error) {
	filePath := filepath.Join(s.rootDir, bucket, key)
	return os.Stat(filePath)
}

// Delete удаляет файл по ключу. Idempotent: если файла нет — возвращает nil.
func (s *FSStore) Delete(bucket, key string) error {
	filePath := filepath.Join(s.rootDir, bucket, key)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil // idempotent: файла не было — это не ошибка
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}
