package wal

import (
	"bytes"
	"testing"
)

func TestWriteAndReadRecord(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello wal")

	// Пишем
	if err := WriteRecord(&buf, payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Читаем
	readPayload, err := ReadRecord(&buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if !bytes.Equal(payload, readPayload) {
		t.Errorf("expected %s, got %s", payload, readPayload)
	}
}

func TestReadCorruptedRecord(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello wal")
	if err := WriteRecord(&buf, payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Портим последний байт (ломаем CRC)
	b := buf.Bytes()
	b[len(b)-1] ^= 0xFF

	_, err := ReadRecord(bytes.NewReader(b))
	if err == nil {
		t.Fatal("expected error on corrupted record")
	}
}
