package blob

import (
	"strings"
	"testing"
)

func TestCalculateSHA256(t *testing.T) {
	// Реальный SHA-256 хэш от строки "Hello, S3!"
	expected := "c9ad25d0e9aa0413bd60f3afd33a35844fca35c99ae4ebcf0f8cdc8df27372ab"

	reader := strings.NewReader("Hello, S3!")

	hash, err := CalculateSHA256(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash != expected {
		t.Errorf("expected %s, got %s", expected, hash)
	}
}

func TestCalculateCRC32C(t *testing.T) {
	// Реальный CRC32C хэш от строки "Hello, S3!"
	expected := "2ead0728"

	reader := strings.NewReader("Hello, S3!")

	hash, err := CalculateCRC32C(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash != expected {
		t.Errorf("expected %s, got %s", expected, hash)
	}
}
