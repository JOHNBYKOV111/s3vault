package blob

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func checkFileExists(t *testing.T, path string, shouldExist bool) {
	t.Helper()
	_, err := os.Stat(path)
	exists := err == nil
	if shouldExist && !exists {
		t.Errorf("expected file to exist: %s", path)
	}
	if !shouldExist && exists {
		t.Errorf("expected file to NOT exist: %s", path)
	}
}

func TestHeal(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	store := NewReplicaStore(dirA, dirB)

	data := []byte("Hello, Self-Heal!")
	bucket := "test-bucket"
	key := "heal-test.txt"

	pathA := filepath.Join(dirA, bucket, key)
	pathB := filepath.Join(dirB, bucket, key)

	// 1. Записываем файл (в обе директории)
	if _, err := store.Put(bucket, key, bytes.NewReader(data)); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	checkFileExists(t, pathA, true)
	checkFileExists(t, pathB, true)

	// 2. Удаляем файл из первой директории (имитация отказа диска)
	if err := os.Remove(pathA); err != nil {
		t.Fatalf("failed to remove file from dir A: %v", err)
	}
	checkFileExists(t, pathA, false)

	// 3. Вызываем Heal (должен восстановить файл из dirB)
	if err := store.Heal(bucket, key); err != nil {
		t.Fatalf("failed to heal: %v", err)
	}

	// 4. Проверяем, что файл снова есть в обеих директориях
	checkFileExists(t, pathA, true)
	checkFileExists(t, pathB, true)

	// 5. Проверяем, что данные совпадают (побайтово)
	reader, err := store.Get(bucket, key)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	defer reader.Close()

	readData, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if !bytes.Equal(data, readData) {
		t.Errorf("expected %q, got %q", data, readData)
	}
}
