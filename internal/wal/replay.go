package wal

import (
	"fmt"
)

// Replay читает WAL и возвращает все записи, которые удалось восстановить.
// Используется после краша для восстановления состояния.
//
// В реальном краше WAL может быть обрезан посередине записи.
// В этом случае Replay читает все целые записи до момента обрыва,
// а на битой/обрезанной записи останавливается и возвращает то, что успел прочитать.
func (w *WAL) Replay() ([][]byte, error) {
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
			// Если запись битая или обрезана (например, после краша) —
			// прерываем чтение и возвращаем то, что успели прочитать.
			break
		}
		records = append(records, record)
	}

	return records, nil
}
