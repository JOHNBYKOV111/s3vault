package s3api

import (
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

// InitiateMultipartUploadResult — ответ на CreateMultipartUpload
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

// CompleteMultipartUploadRequest — запрос на завершение
type CompleteMultipartUploadRequest struct {
	XMLName xml.Name `xml:"CompleteMultipartUpload"`
	Parts   []Part   `xml:"Part"`
}

type Part struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// multipartDir — директория для хранения частей multipart.
const multipartDir = "./multipart"

// HandleCreateMultipartUpload обрабатывает POST /bucket/key?uploads
func (h *ObjectHandler) HandleCreateMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	pgRepo, ok := h.repo.(*metadata.PostgresRepo)
	if !ok {
		WriteError(w, "InternalError", "Repository is not Postgres", http.StatusInternalServerError)
		return
	}

	// Находим bucket_id
	var bucketID string
	err := pgRepo.GetPool().QueryRow(r.Context(), "SELECT id FROM buckets WHERE name = $1", bucket).Scan(&bucketID)
	if err != nil {
		WriteError(w, "NoSuchBucket", "Bucket not found", http.StatusNotFound)
		return
	}

	var uploadID string
	err = pgRepo.GetPool().QueryRow(r.Context(),
		"INSERT INTO multipart_uploads (bucket_id, key) VALUES ($1, $2) RETURNING id",
		bucketID, key).Scan(&uploadID)
	if err != nil {
		WriteError(w, "InternalError", "Failed to create upload: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Создаем директорию для частей
	uploadDir := filepath.Join(multipartDir, uploadID)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		WriteError(w, "InternalError", "Failed to create upload dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := InitiateMultipartUploadResult{
		Bucket:   bucket,
		Key:      key,
		UploadID: uploadID,
	}

	w.Header().Set("X-Amz-Upload-Id", uploadID)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	if err := xml.NewEncoder(w).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: encode InitiateMultipartUploadResult: %v\n", err)
	}
}

// HandleUploadPart обрабатывает PUT /bucket/key?partNumber=N&uploadId=X
func (h *ObjectHandler) HandleUploadPart(w http.ResponseWriter, r *http.Request, bucket, key string) {
	partNumberStr := r.URL.Query().Get("partNumber")
	uploadID := r.URL.Query().Get("uploadId")

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 || partNumber > 10000 {
		WriteError(w, "InvalidArgument", "Invalid partNumber", http.StatusBadRequest)
		return
	}

	pgRepo, ok := h.repo.(*metadata.PostgresRepo)
	if !ok {
		WriteError(w, "InternalError", "Repository is not Postgres", http.StatusInternalServerError)
		return
	}

	// Проверяем, что upload существует
	var exists bool
	err = pgRepo.GetPool().QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM multipart_uploads WHERE id = $1)", uploadID).Scan(&exists)
	if err != nil || !exists {
		WriteError(w, "NoSuchUpload", "Upload not found", http.StatusNotFound)
		return
	}

	// Сохраняем часть на диск (стриминг)
	partPath := filepath.Join(multipartDir, uploadID, fmt.Sprintf("%d", partNumber))
	file, err := os.Create(partPath)
	if err != nil {
		WriteError(w, "InternalError", "Failed to create part file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()

	// Считаем SHA-256 и размер на лету
	hasher := sha256.New()
	teeReader := io.TeeReader(r.Body, hasher)

	size, err := io.Copy(file, teeReader)
	if err != nil {
		WriteError(w, "InternalError", "Failed to write part: "+err.Error(), http.StatusInternalServerError)
		return
	}

	etag := fmt.Sprintf("\"%x\"", hasher.Sum(nil))

	// Сохраняем метаданные части в базу (без data)
	_, err = pgRepo.GetPool().Exec(r.Context(),
		`INSERT INTO parts (upload_id, part_number, size, etag) 
		 VALUES ($1, $2, $3, $4) 
		 ON CONFLICT (upload_id, part_number) 
		 DO UPDATE SET size = EXCLUDED.size, etag = EXCLUDED.etag`,
		uploadID, partNumber, size, etag)
	if err != nil {
		WriteError(w, "InternalError", "Failed to save part metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

// HandleCompleteMultipartUpload обрабатывает POST /bucket/key?uploadId=X
func (h *ObjectHandler) HandleCompleteMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")

	var completeReq CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&completeReq); err != nil {
		WriteError(w, "MalformedXML", "Invalid XML", http.StatusBadRequest)
		return
	}

	pgRepo, ok := h.repo.(*metadata.PostgresRepo)
	if !ok {
		WriteError(w, "InternalError", "Repository is not Postgres", http.StatusInternalServerError)
		return
	}

	// Создаем директорию для итогового объекта
	bucketDir := filepath.Join("./data", bucket)
	if err := os.MkdirAll(bucketDir, 0755); err != nil {
		WriteError(w, "InternalError", "Failed to create bucket dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Открываем файл для итогового объекта
	objectPath := filepath.Join(bucketDir, key)
	objectFile, err := os.Create(objectPath)
	if err != nil {
		WriteError(w, "InternalError", "Failed to create object file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = objectFile.Close() }()

	var totalSize int64
	hasher := sha256.New()
	multiWriter := io.MultiWriter(objectFile, hasher)

	for _, part := range completeReq.Parts {
		// Проверяем ETag части
		var partETag string
		err := pgRepo.GetPool().QueryRow(r.Context(),
			"SELECT etag FROM parts WHERE upload_id = $1 AND part_number = $2",
			uploadID, part.PartNumber).Scan(&partETag)
		if err != nil {
			WriteError(w, "InvalidPart", fmt.Sprintf("Part %d not found", part.PartNumber), http.StatusBadRequest)
			return
		}

		clientETag := strings.Trim(part.ETag, `"`)
		storedETag := strings.Trim(partETag, `"`)

		if clientETag != storedETag {
			WriteError(w, "InvalidPart", fmt.Sprintf("ETag mismatch for part %d", part.PartNumber), http.StatusBadRequest)
			return
		}

		// Открываем файл части и стримим в итоговый объект
		partPath := filepath.Join(multipartDir, uploadID, fmt.Sprintf("%d", part.PartNumber))
		partFile, err := os.Open(partPath)
		if err != nil {
			WriteError(w, "InternalError", fmt.Sprintf("Failed to open part %d: %v", part.PartNumber, err), http.StatusInternalServerError)
			return
		}

		n, err := io.Copy(multiWriter, partFile)
		if closeErr := partFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "s3api: close part file %d: %v\n", part.PartNumber, closeErr)
		}
		if err != nil {
			WriteError(w, "InternalError", fmt.Sprintf("Failed to copy part %d: %v", part.PartNumber, err), http.StatusInternalServerError)
			return
		}
		totalSize += n
	}

	// Сохраняем метаданные объекта в базу
	var bucketID string
	err = pgRepo.GetPool().QueryRow(r.Context(), "SELECT id FROM buckets WHERE name = $1", bucket).Scan(&bucketID)
	if err != nil {
		WriteError(w, "InternalError", "Bucket not found", http.StatusInternalServerError)
		return
	}

	etag := fmt.Sprintf("\"%x\"", hasher.Sum(nil))

	// Передаем content_type и encryption_key_id (пустой, т.к. multipart пока не шифруется)
	_, err = pgRepo.GetPool().Exec(r.Context(),
		`INSERT INTO objects (bucket_id, key, size, etag, content_type, encryption_key_id) 
		 VALUES ($1, $2, $3, $4, $5, $6) 
		 ON CONFLICT (bucket_id, key) 
		 DO UPDATE SET size = EXCLUDED.size, etag = EXCLUDED.etag, content_type = EXCLUDED.content_type, encryption_key_id = EXCLUDED.encryption_key_id`,
		bucketID, key, totalSize, etag, "application/octet-stream", nil)
	if err != nil {
		WriteError(w, "InternalError", "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Удаляем upload и все его части
	if _, err := pgRepo.GetPool().Exec(r.Context(), "DELETE FROM multipart_uploads WHERE id = $1", uploadID); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: delete multipart_upload %s: %v\n", uploadID, err)
	}
	if err := os.RemoveAll(filepath.Join(multipartDir, uploadID)); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: remove multipart dir %s: %v\n", uploadID, err)
	}

	w.WriteHeader(http.StatusOK)
}

// HandleAbortMultipartUpload обрабатывает DELETE /bucket/key?uploadId=X
func (h *ObjectHandler) HandleAbortMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")

	pgRepo, ok := h.repo.(*metadata.PostgresRepo)
	if !ok {
		WriteError(w, "InternalError", "Repository is not Postgres", http.StatusInternalServerError)
		return
	}

	tag, err := pgRepo.GetPool().Exec(r.Context(), "DELETE FROM multipart_uploads WHERE id = $1", uploadID)
	if err != nil {
		WriteError(w, "InternalError", "Failed to abort upload: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if tag.RowsAffected() == 0 {
		WriteError(w, "NoSuchUpload", "Upload not found", http.StatusNotFound)
		return
	}

	// Удаляем директорию с частями
	if err := os.RemoveAll(filepath.Join(multipartDir, uploadID)); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: remove multipart dir %s: %v\n", uploadID, err)
	}

	w.WriteHeader(http.StatusNoContent)
}
