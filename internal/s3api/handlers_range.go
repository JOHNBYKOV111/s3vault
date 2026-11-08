package s3api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// RangeRequest содержит распарсенный заголовок Range
type RangeRequest struct {
	Start int64
	End   int64
}

// ParseRange парсит заголовок Range (например, "bytes=0-1023").
// Возвращает nil, если заголовок пустой или невалидный.
func ParseRange(rangeHeader string, fileSize int64) (*RangeRequest, error) {
	if rangeHeader == "" {
		return nil, nil // Нет заголовка — отдаем весь файл
	}

	// S3 поддерживает только формат "bytes=start-end"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, fmt.Errorf("invalid range header: %s", rangeHeader)
	}

	rangeStr := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format: %s", rangeStr)
	}

	var start, end int64
	var err error

	// Парсим начало
	if parts[0] != "" {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid start: %s", parts[0])
		}
	}

	// Парсим конец
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid end: %s", parts[1])
		}
	} else {
		// Если конец не указан (например, "bytes=0-"), читаем до конца файла
		end = fileSize - 1
	}

	// Валидация: начало не может быть больше конца, конец не может быть больше размера файла
	if start > end || start < 0 {
		return nil, fmt.Errorf("invalid range: start=%d, end=%d", start, end)
	}
	if end >= fileSize {
		end = fileSize - 1
	}

	return &RangeRequest{Start: start, End: end}, nil
}

// HandleRange отдает часть файла (206 Partial Content).
// Вызывается из handleGet, если есть заголовок Range.
func (h *ObjectHandler) HandleRange(w http.ResponseWriter, r *http.Request, reader io.ReadSeeker, fileSize int64, bucket, key string) {
	rangeHeader := r.Header.Get("Range")

	rangeReq, err := ParseRange(rangeHeader, fileSize)
	if err != nil {
		WriteError(w, "InvalidRange", err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Если Range невалидный или отсутствует — отдаем весь файл
	if rangeReq == nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, reader); err != nil {
			// Заголовки уже отправлены, клиенту ошибку не сообщить.
			fmt.Fprintf(os.Stderr, "s3api: io.Copy in HandleRange (full): %v\n", err)
		}
		return
	}

	// Сдвигаем курсор файла на начало диапазона
	if _, err := reader.Seek(rangeReq.Start, io.SeekStart); err != nil {
		WriteError(w, "InternalError", "Failed to seek: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Вычисляем длину куска
	length := rangeReq.End - rangeReq.Start + 1

	// Устанавливаем заголовки для 206 Partial Content
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeReq.Start, rangeReq.End, fileSize))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", length))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusPartialContent)

	// Отдаем только запрошенный кусок
	if _, err := io.CopyN(w, reader, length); err != nil {
		// Заголовки уже отправлены, клиенту ошибку не сообщить.
		fmt.Fprintf(os.Stderr, "s3api: io.CopyN in HandleRange (partial): %v\n", err)
	}
}
