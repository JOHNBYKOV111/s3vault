package wal

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// WAL — журнал предварительной записи с group commit.
type WAL struct {
	file      *os.File
	mu        sync.Mutex
	buffer    [][]byte
	commitCh  chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewWAL создает новый WAL.
func NewWAL(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open WAL file: %w", err)
	}

	w := &WAL{
		file:     file,
		commitCh: make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
	}

	// Запускаем фоновую горутину для group commit
	w.wg.Add(1)
	go w.groupCommitLoop()

	return w, nil
}

// Append добавляет запись в WAL.
// Запись попадает в буфер, а fsync происходит батчами (group commit).
func (w *WAL) Append(record []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closeCh:
		return fmt.Errorf("WAL is closed")
	default:
	}

	// Добавляем в буфер
	w.buffer = append(w.buffer, record)

	// Сигналим фоновой горутине, что есть новые данные
	select {
	case w.commitCh <- struct{}{}:
	default:
	}

	return nil
}

// groupCommitLoop — фоновая горутина, которая делает fsync батчами.
func (w *WAL) groupCommitLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond) // fsync каждые 10 мс
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.flush()
		case <-w.closeCh:
			w.flush() // финальный fsync
			return
		}
	}
}

// flush записывает буфер на диск и делает fsync.
func (w *WAL) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buffer) == 0 {
		return
	}

	// Записываем все записи в файл
	for _, record := range w.buffer {
		if err := WriteRecord(w.file, record); err != nil {
			// В реальном коде нужно логировать ошибку
			continue
		}
	}

	// Делаем fsync (group commit)
	if err := w.file.Sync(); err != nil {
		// В реальном коде нужно логировать ошибку
	}

	// Очищаем буфер
	w.buffer = nil
}

// ReadAll читает все записи из WAL (с диска).
func (w *WAL) ReadAll() ([][]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Перемещаемся в начало файла
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("seek: %w", err)
	}

	var records [][]byte
	for {
		record, err := ReadRecord(w.file)
		if err != nil {
			// Если достигли конца файла — выходим
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("read record: %w", err)
		}
		records = append(records, record)
	}

	return records, nil
}

// Close закрывает WAL (делает финальный fsync).
func (w *WAL) Close() error {
	w.closeOnce.Do(func() {
		close(w.closeCh)
	})
	w.wg.Wait()
	return w.file.Close()
}

// --- Вспомогательные функции для тестов ---

// IsEmpty проверяет, пуст ли буфер.
func (w *WAL) IsEmpty() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.buffer) == 0
}

// Bytes возвращает размер буфера (для тестов).
func (w *WAL) Bytes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	var size int
	for _, rec := range w.buffer {
		size += len(rec)
	}
	return size
}
