package s3api

import (
	"errors"
	"strings"
)

// ParseS3Path разбирает путь вида /bucket/key на бакет и ключ.
func ParseS3Path(path string) (bucket string, key string, err error) {
	// Защита от абсолютных путей и двойных слэшей (например, //etc/passwd)
	if strings.Contains(path, "//") {
		return "", "", errors.New("absolute path or double slashes detected")
	}

	// Убираем ВСЕ ведущие слэши у всего пути
	path = strings.TrimLeft(path, "/")

	if path == "" {
		return "", "", errors.New("empty path")
	}

	// Защита от path traversal (выход за пределы папки)
	if strings.Contains(path, "..") {
		return "", "", errors.New("path traversal detected")
	}

	parts := strings.SplitN(path, "/", 2)
	bucket = parts[0]

	// Проверяем, что бакет не пустой
	if bucket == "" {
		return "", "", errors.New("empty bucket")
	}

	if len(parts) > 1 {
		// Убираем ведущие слэши из ключа (например, "bucket//file" -> "file")
		key = strings.TrimLeft(parts[1], "/")
	}

	return bucket, key, nil
}
