package blob

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReplicaStore(t *testing.T) {
	// 1. Создаем две временные директории (две "ноды")
	dirA := t.TempDir()
	dirB := t.TempDir()

	// 2. Создаем реплицированное хранилище
	store := NewReplicaStore(dirA, dirB)

	// 3. Записываем файл
	data := []byte("Hello, Replicated S3!")
	bucket := "test-bucket"
	key := "test-key.txt"

	n, err := store.Put(bucket, key, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to put: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	// 4. Проверяем, что файл есть в обеих директориях
	pathA := filepath.Join(dirA, bucket, key)
	pathB := filepath.Join(dirB, bucket, key)

	if _, err := os.Stat(pathA); os.IsNotExist(err) {
		t.Errorf("file not found in dir A: %s", pathA)
	}
	if _, err := os.Stat(pathB); os.IsNotExist(err) {
		t.Errorf("file not found in dir B: %s", pathB)
	}

	// 5. Читаем файл (должен читаться из первой доступной копии)
	reader, err := store.Get(bucket, key)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	defer func() { _ = reader.Close() }()

	readData, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if !bytes.Equal(data, readData) {
		t.Errorf("expected %s, got %s", data, readData)
	}

	// 6. Удаляем файл из первой директории (имитация отказа ноды)
	if err := os.Remove(pathA); err != nil {
		t.Fatalf("failed to remove file from dir A: %v", err)
	}

	// 7. Читаем снова (должен прочитать из второй копии)
	reader2, err := store.Get(bucket, key)
	if err != nil {
		t.Fatalf("failed to get after removing dir A: %v", err)
	}
	defer func() { _ = reader2.Close() }()

	readData2, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("failed to read after removing dir A: %v", err)
	}
	if !bytes.Equal(data, readData2) {
		t.Errorf("expected %s, got %s", data, readData2)
	}
}
