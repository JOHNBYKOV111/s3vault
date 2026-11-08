package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"io"
)

// CalculateSHA256 читает данные из reader потоком и возвращает SHA-256 хэш в hex-формате.
func CalculateSHA256(r io.Reader) (string, error) {
	hasher := sha256.New()

	// io.Copy читает данные кусками и пишет их в hasher.
	// Это не загружает весь файл в память (O(chunk) RAM).
	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// CalculateCRC32C читает данные из reader потоком и возвращает CRC32C хэш в hex-формате.
// CRC32C (Castagnoli) — это стандарт, который использует Amazon S3.
func CalculateCRC32C(r io.Reader) (string, error) {
	// Используем таблицу с полиномом Кастаньоли
	table := crc32.MakeTable(crc32.Castagnoli)
	hasher := crc32.New(table)

	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
