package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// GenerateKey генерирует пару ключей: accessKey (публичный) и secretKey (секретный).
// Возвращает accessKey, secretKey и ошибку.
func GenerateKey() (string, string, error) {
	// 1. Генерируем случайный accessKey (16 байт)
	accessBytes := make([]byte, 16)
	if _, err := rand.Read(accessBytes); err != nil {
		return "", "", fmt.Errorf("generate access key: %w", err)
	}
	accessKey := "AKIA" + base64.RawURLEncoding.EncodeToString(accessBytes)

	// 2. Генерируем случайный secretKey (32 байта)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("generate secret key: %w", err)
	}
	secretKey := base64.RawURLEncoding.EncodeToString(secretBytes)

	return accessKey, secretKey, nil
}

// HashSecret хэширует secretKey (bcrypt) для хранения в БД.
func HashSecret(secretKey string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secretKey), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash secret: %w", err)
	}
	return string(hash), nil
}

// VerifySecret проверяет, что secretKey соответствует хэшу.
func VerifySecret(secretKey, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secretKey))
	return err == nil
}

// HashAccessKey хэширует accessKey (SHA-256) для быстрого поиска в БД.
// (Для accessKey используем SHA-256, а не bcrypt, потому что accessKey — публичный,
// и его нужно быстро искать по индексу.)
func HashAccessKey(accessKey string) string {
	hash := sha256.Sum256([]byte(accessKey))
	return hex.EncodeToString(hash[:])
}
