package s3api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/crypto"
	"github.com/johnbykov1989/s3vault/internal/metadata"
)

type ObjectHandler struct {
	store blob.Store
	repo  metadata.Repository
	env   *crypto.Envelope // Envelope encryption (может быть nil)
}

func NewObjectHandler(store blob.Store, repo metadata.Repository) *ObjectHandler {
	return &ObjectHandler{store: store, repo: repo}
}

// WithEnvelope добавляет envelope encryption в хендлер.
func (h *ObjectHandler) WithEnvelope(env *crypto.Envelope) *ObjectHandler {
	h.env = env
	return h
}

func (h *ObjectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
		return
	}

	if r.URL.Path == "/" && r.Method == http.MethodGet {
		h.HandleListBuckets(w, r)
		return
	}

	// Multipart: CreateMultipartUpload (POST ?uploads)
	if r.Method == http.MethodPost {
		if _, ok := r.URL.Query()["uploads"]; ok {
			bucket, key, err := ParseS3Path(r.URL.Path)
			if err != nil {
				WriteError(w, "InvalidPath", err.Error(), http.StatusBadRequest)
				return
			}
			h.HandleCreateMultipartUpload(w, r, bucket, key)
			return
		}
	}

	// Multipart: UploadPart (PUT ?partNumber=N&uploadId=X)
	if r.Method == http.MethodPut {
		if _, ok := r.URL.Query()["partNumber"]; ok {
			bucket, key, err := ParseS3Path(r.URL.Path)
			if err != nil {
				WriteError(w, "InvalidPath", err.Error(), http.StatusBadRequest)
				return
			}
			h.HandleUploadPart(w, r, bucket, key)
			return
		}
	}

	// Multipart: CompleteMultipartUpload (POST ?uploadId=X)
	if r.Method == http.MethodPost {
		if _, ok := r.URL.Query()["uploadId"]; ok {
			bucket, key, err := ParseS3Path(r.URL.Path)
			if err != nil {
				WriteError(w, "InvalidPath", err.Error(), http.StatusBadRequest)
				return
			}
			h.HandleCompleteMultipartUpload(w, r, bucket, key)
			return
		}
	}

	// Multipart: AbortMultipartUpload (DELETE ?uploadId=X)
	if r.Method == http.MethodDelete {
		if _, ok := r.URL.Query()["uploadId"]; ok {
			bucket, key, err := ParseS3Path(r.URL.Path)
			if err != nil {
				WriteError(w, "InvalidPath", err.Error(), http.StatusBadRequest)
				return
			}
			h.HandleAbortMultipartUpload(w, r, bucket, key)
			return
		}
	}

	// Обычные запросы
	bucket, key, err := ParseS3Path(r.URL.Path)
	if err != nil {
		WriteError(w, "InvalidPath", err.Error(), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPut && key == "" {
		h.HandleCreateBucket(w, r, bucket)
		return
	}

	if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
		h.HandleListObjectsV2(w, r, bucket)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.handlePut(w, r, bucket, key)
	case http.MethodGet, http.MethodHead:
		h.handleGet(w, r, bucket, key)
	case http.MethodDelete:
		h.handleDelete(w, r, bucket, key)
	default:
		WriteError(w, "MethodNotAllowed", "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ObjectHandler) handlePut(w http.ResponseWriter, r *http.Request, bucket, key string) {
	hasher := sha256.New()
	teeReader := io.TeeReader(r.Body, hasher)

	// 1. Если envelope encryption включен — шифруем данные
	dataToStore := io.Reader(teeReader)
	var keyID string

	if h.env != nil {
		plaintext, err := io.ReadAll(teeReader)
		if err != nil {
			WriteError(w, "InternalError", "Failed to read body: "+err.Error(), http.StatusInternalServerError)
			return
		}

		ciphertext, kid, err := h.env.Encrypt(plaintext)
		if err != nil {
			WriteError(w, "InternalError", "Failed to encrypt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		keyID = kid
		dataToStore = strings.NewReader(string(ciphertext))
	}

	// 2. Сохраняем данные на диск
	size, err := h.store.Put(bucket, key, dataToStore)
	if err != nil {
		WriteError(w, "InternalError", "Failed to save object: "+err.Error(), http.StatusInternalServerError)
		return
	}

	hash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Сохраняем метаданные в базу
	if h.repo != nil {
		tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
		if val := r.Context().Value(tenantIDKey); val != nil {
			if s, ok := val.(string); ok {
				tenantID = s
			}
		}

		b, err := h.repo.GetBucket(r.Context(), tenantID, bucket)
		if err != nil {
			WriteError(w, "NoSuchBucket", "The specified bucket does not exist.", http.StatusNotFound)
			return
		}

		contentType := r.Header.Get("Content-Type")
		_, err = h.repo.PutObject(r.Context(), b.ID, key, size, hash, contentType, keyID)
		if err != nil {
			WriteError(w, "InternalError", "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", hash))
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (h *ObjectHandler) handleGet(w http.ResponseWriter, r *http.Request, bucket, key string) {
	reader, err := h.store.Get(bucket, key)
	if err != nil {
		if os.IsNotExist(err) {
			WriteError(w, "NoSuchKey", "The specified key does not exist.", http.StatusNotFound)
			return
		}
		WriteError(w, "InternalError", "Failed to get object: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = reader.Close() }()

	seeker, ok := reader.(io.ReadSeeker)
	if !ok {
		WriteError(w, "InternalError", "Storage does not support seeking", http.StatusInternalServerError)
		return
	}

	fileSize, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		WriteError(w, "InternalError", "Failed to get file size", http.StatusInternalServerError)
		return
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		WriteError(w, "InternalError", "Failed to seek to start", http.StatusInternalServerError)
		return
	}

	info, err := h.store.Stat(bucket, key)
	if err == nil {
		w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	}

	// Получаем ETag из базы
	if h.repo != nil {
		tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
		if val := r.Context().Value(tenantIDKey); val != nil {
			if s, ok := val.(string); ok {
				tenantID = s
			}
		}

		b, err := h.repo.GetBucket(r.Context(), tenantID, bucket)
		if err == nil {
			obj, err := h.repo.GetObject(r.Context(), b.ID, key)
			if err == nil {
				cleanETag := strings.Trim(obj.ETag, `"`)
				w.Header().Set("ETag", fmt.Sprintf("\"%s\"", cleanETag))
			}
		}
	}

	w.Header().Set("Accept-Ranges", "bytes")

	// Range-запрос: Content-Length и Content-Range выставляет ТОЛЬКО HandleRange.
	if r.Header.Get("Range") != "" {
		h.HandleRange(w, r, seeker, fileSize, bucket, key)
		return
	}

	// Обычный GET/HEAD: Content-Length = размер файла.
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: io.Copy in handleGet: %v\n", err)
	}
}

// handleDelete удаляет объект: metadata (БД) и blob (диск).
// S3-совместимый ответ: 204 No Content (даже если объекта не было — idempotent).
func (h *ObjectHandler) handleDelete(w http.ResponseWriter, r *http.Request, bucket, key string) {
	// 1. Удаляем blob с диска (если есть).
	if err := h.store.Delete(bucket, key); err != nil {
		if !os.IsNotExist(err) {
			WriteError(w, "InternalError", "Failed to delete blob: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Если blob'а нет — это ок (idempotent DELETE).
	}

	// 2. Удаляем metadata из БД (если repo подключён).
	if h.repo != nil {
		tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
		if val := r.Context().Value(tenantIDKey); val != nil {
			if s, ok := val.(string); ok {
				tenantID = s
			}
		}

		b, err := h.repo.GetBucket(r.Context(), tenantID, bucket)
		if err == nil {
			if err := h.repo.DeleteObject(r.Context(), b.ID, key); err != nil {
				if err != metadata.ErrNotFound {
					WriteError(w, "InternalError", "Failed to delete metadata: "+err.Error(), http.StatusInternalServerError)
					return
				}
				// Если объекта в БД нет — idempotent, ок.
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
