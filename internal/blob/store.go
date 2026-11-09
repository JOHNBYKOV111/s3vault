package blob

import (
	"io"
	"os"
)

// Store — это интерфейс для работы с хранилищем.
// Позже мы сможем подменить FS на S3 или репликацию.
type Store interface {
	// Put сохраняет данные по ключу. Возвращает количество записанных байт.
	Put(bucket, key string, r io.Reader) (int64, error)

	// Get читает данные по ключу.
	Get(bucket, key string) (io.ReadCloser, error)

	// List возвращает список ключей (файлов) в указанном бакете.
	List(bucket string) ([]string, error)

	// Stat возвращает информацию о файле (размер, время изменения).
	Stat(bucket, key string) (os.FileInfo, error)

	// Delete удаляет данные по ключу.
	// Возвращает nil, если файл удалён или его не было (idempotent).
	Delete(bucket, key string) error
}
