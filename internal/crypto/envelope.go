package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// Envelope — envelope encryption (KEK шифрует DEK, DEK шифрует данные).
type Envelope struct {
	kek []byte
}

// NewEnvelope создает envelope encryption с указанным KEK.
func NewEnvelope(kek []byte) (*Envelope, error) {
	if len(kek) != 32 {
		return nil, fmt.Errorf("KEK must be 32 bytes (AES-256), got %d", len(kek))
	}
	return &Envelope{kek: kek}, nil
}

// Encrypt шифрует данные, используя DEK, и возвращает шифротекст + keyID.
func (e *Envelope) Encrypt(plaintext []byte) ([]byte, string, error) {
	// 1. Генерируем DEK (Data Encryption Key)
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, "", fmt.Errorf("generate DEK: %w", err)
	}

	// 2. Вычисляем keyID (хэш DEK, чтобы можно было найти KEK)
	keyIDHash := sha256.Sum256(dek)
	keyID := hex.EncodeToString(keyIDHash[:8]) // первые 8 байт

	// 3. Шифруем DEK с помощью KEK
	dekCiphertext, err := encryptAESGCM(e.kek, dek)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt DEK: %w", err)
	}

	// 4. Шифруем данные с помощью DEK
	dataCiphertext, err := encryptAESGCM(dek, plaintext)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt data: %w", err)
	}

	// 5. Формат: [dekCiphertext (len+nonce+data)] + [dataCiphertext]
	// Для простоты: длина DEK-шифротекста (2 байта) + DEK-шифротекст + данные
	result := make([]byte, 0, 2+len(dekCiphertext)+len(dataCiphertext))
	result = append(result, byte(len(dekCiphertext)>>8), byte(len(dekCiphertext)))
	result = append(result, dekCiphertext...)
	result = append(result, dataCiphertext...)

	return result, keyID, nil
}

// Decrypt расшифровывает данные, используя keyID (для поиска KEK).
func (e *Envelope) Decrypt(ciphertext []byte, keyID string) ([]byte, error) {
	if len(ciphertext) < 2 {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// 1. Извлекаем DEK-шифротекст
	dekLen := int(ciphertext[0])<<8 | int(ciphertext[1])
	if len(ciphertext) < 2+dekLen {
		return nil, fmt.Errorf("ciphertext too short for DEK")
	}
	dekCiphertext := ciphertext[2 : 2+dekLen]
	dataCiphertext := ciphertext[2+dekLen:]

	// 2. Расшифровываем DEK с помощью KEK
	dek, err := decryptAESGCM(e.kek, dekCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK: %w", err)
	}

	// 3. Расшифровываем данные с помощью DEK
	plaintext, err := decryptAESGCM(dek, dataCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt data: %w", err)
	}

	return plaintext, nil
}

// encryptAESGCM шифрует данные с помощью AES-256-GCM.
func encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptAESGCM расшифровывает данные с помощью AES-256-GCM.
func decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
