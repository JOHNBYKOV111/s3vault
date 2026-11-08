package crypto

import (
	"bytes"
	"testing"
)

func TestEnvelopeEncryption(t *testing.T) {
	// 1. Создаем KEK (Key Encryption Key) — 32 байта для AES-256
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}

	env, err := NewEnvelope(kek)
	if err != nil {
		t.Fatalf("failed to create envelope: %v", err)
	}

	// 2. Шифруем данные
	plaintext := []byte("Hello, Envelope Encryption!")
	ciphertext, keyID, err := env.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	if keyID == "" {
		t.Error("expected keyID, got empty")
	}

	// 3. Расшифровываем данные (должны совпадать побайтово)
	decrypted, err := env.Decrypt(ciphertext, keyID)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}

	// 4. Подменяем байт в шифротексте (имитация bit rot / подмены)
	ciphertext[0] ^= 0xFF
	_, err = env.Decrypt(ciphertext, keyID)
	if err == nil {
		t.Error("expected error on corrupted ciphertext, got nil")
	}
}
