package wal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWAL_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := NewWAL(walPath)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	defer func() {
		if err := wal.Close(); err != nil {
			t.Errorf("failed to close WAL: %v", err)
		}
	}()

	// 1. Пишем несколько записей
	records := [][]byte{
		[]byte("first record"),
		[]byte("second record"),
		[]byte("third record"),
	}

	for _, rec := range records {
		if err := wal.Append(rec); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// 2. Ждем, пока group commit сработает (fsync)
	// Даем достаточно времени, чтобы flush гарантированно записал на диск
	time.Sleep(100 * time.Millisecond)

	// 3. Читаем все записи с диска
	readRecords, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	// 4. Проверяем, что количество совпадает
	if len(readRecords) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(readRecords))
	}

	// 5. Проверяем, что данные ТОЧНО совпадают (побайтово)
	for i, rec := range records {
		if !bytes.Equal(readRecords[i], rec) {
			t.Errorf("record %d: expected %q, got %q", i, rec, readRecords[i])
		}
	}
}

func TestWAL_GroupCommit(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	wal, err := NewWAL(walPath)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	// 1. Пишем записи в быстром темпе (group commit должен сработать)
	for i := 0; i < 100; i++ {
		if err := wal.Append([]byte("record")); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// 2. Ждем, пока group commit сработает (fsync)
	time.Sleep(100 * time.Millisecond)

	// 3. Закрываем WAL (должен сделать финальный fsync)
	if err := wal.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// 4. Проверяем, что файл существует и не пустой
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("failed to stat WAL: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-empty WAL file")
	}
}
