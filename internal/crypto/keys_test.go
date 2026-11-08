package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKEK(t *testing.T) {
	dir := t.TempDir()
	kekPath := filepath.Join(dir, "kek.key")

	// 1. Создаем файл с KEK (32 байта)
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	if err := os.WriteFile(kekPath, kek, 0400); err != nil {
		t.Fatalf("failed to write KEK: %v", err)
	}

	// 2. Загружаем KEK
	loaded, err := LoadKEK(kekPath)
	if err != nil {
		t.Fatalf("failed to load KEK: %v", err)
	}
	if len(loaded) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(loaded))
	}
	if loaded[0] != 0 || loaded[31] != 31 {
		t.Errorf("KEK data mismatch")
	}

	// 3. Проверяем ошибку, если файл не существует
	_, err = LoadKEK(filepath.Join(dir, "nonexistent.key"))
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}

	// 4. Проверяем ошибку, если права файла не 0400
	badPath := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(badPath, kek, 0644); err != nil {
		t.Fatalf("failed to write bad KEK: %v", err)
	}
	_, err = LoadKEK(badPath)
	if err == nil {
		t.Error("expected error for too-open permissions, got nil")
	}
}
