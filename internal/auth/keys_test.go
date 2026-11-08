package auth

import (
	"testing"
)

func TestKeys(t *testing.T) {
	// 1. Создаем ключ доступа
	accessKey, secretKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if accessKey == "" || secretKey == "" {
		t.Fatal("expected non-empty accessKey and secretKey")
	}

	// 2. Хэшируем секретный ключ (в БД хранится только хэш)
	hash, err := HashSecret(secretKey)
	if err != nil {
		t.Fatalf("failed to hash secret: %v", err)
	}
	if hash == secretKey {
		t.Error("hash must not equal secret key")
	}

	// 3. Проверяем правильный ключ (verify → ok)
	if !VerifySecret(secretKey, hash) {
		t.Error("expected verify to succeed for correct secret")
	}

	// 4. Проверяем неправильный ключ (verify → fail)
	if VerifySecret("wrong-secret", hash) {
		t.Error("expected verify to fail for wrong secret")
	}
}
