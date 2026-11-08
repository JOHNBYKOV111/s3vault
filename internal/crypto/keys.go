package crypto

import (
	"fmt"
	"os"
)

// LoadKEK загружает KEK (Key Encryption Key) из файла.
// Файл должен иметь права 0400 (только чтение для владельца).
func LoadKEK(path string) ([]byte, error) {
	// 1. Проверяем права файла (должны быть 0400)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat KEK file: %w", err)
	}
	if info.Mode().Perm() != 0400 {
		return nil, fmt.Errorf("KEK file must have 0400 permissions, got %o", info.Mode().Perm())
	}

	// 2. Читаем файл
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read KEK file: %w", err)
	}

	// 3. Проверяем длину (32 байта для AES-256)
	if len(data) != 32 {
		return nil, fmt.Errorf("KEK must be 32 bytes, got %d", len(data))
	}

	return data, nil
}
