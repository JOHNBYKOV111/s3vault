package wal

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestReplayAfterSIGKILL проверяет восстановление после SIGKILL.
func TestReplayAfterSIGKILL(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	// 1. Запускаем дочерний процесс, который пишет в WAL и висит
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(), "WAL_PATH="+walPath, "GO_WANT_HELPER_PROCESS=1")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}

	// Ждем, пока процесс запишет данные в WAL (даем 200 мс)
	time.Sleep(200 * time.Millisecond)

	// 2. Убиваем процесс SIGKILL (имитация краша)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		// Процесс убит SIGKILL, Wait вернет *exec.ExitError. Это ожидаемо.
		// Логируем для диагностики, но не считаем ошибкой теста.
		t.Logf("helper process exited: %v", err)
	}

	// 3. Открываем WAL и запускаем Replay (восстановление)
	wal, err := NewWAL(walPath)
	if err != nil {
		t.Fatalf("failed to open WAL: %v", err)
	}
	defer func() {
		if err := wal.Close(); err != nil {
			t.Errorf("failed to close WAL: %v", err)
		}
	}()

	records, err := wal.Replay()
	if err != nil {
		t.Fatalf("failed to replay WAL: %v", err)
	}

	// 4. Проверяем, что записи есть и они корректны
	if len(records) == 0 {
		t.Fatal("expected records in WAL after replay, got 0")
	}

	expected := []byte("hello after crash")
	found := false
	for _, rec := range records {
		if bytes.Equal(rec, expected) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find %q in WAL records, got %v", expected, records)
	}
}

// TestHelperProcess — вспомогательный процесс для теста.
// Он не является частью тестов, а запускается через exec.Command.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	walPath := os.Getenv("WAL_PATH")
	if walPath == "" {
		fmt.Fprintln(os.Stderr, "WAL_PATH not set")
		os.Exit(1)
	}

	wal, err := NewWAL(walPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open WAL: %v\n", err)
		os.Exit(1)
	}

	// Пишем запись и висим (ждем SIGKILL)
	if err := wal.Append([]byte("hello after crash")); err != nil {
		fmt.Fprintf(os.Stderr, "failed to append: %v\n", err)
		os.Exit(1)
	}

	// Ждем вечно (пока не убьют)
	select {}
}
