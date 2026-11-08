package s3api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/johnbykov1989/s3vault/internal/metadata"
)

// ListBucketResult — структура для XML-ответа S3 (список объектов в бакете)
type ListBucketResult struct {
	XMLName     xml.Name `xml:"ListBucketResult"`
	Name        string   `xml:"Name"`
	Prefix      string   `xml:"Prefix"`
	KeyCount    int      `xml:"KeyCount"`
	MaxKeys     int      `xml:"MaxKeys"`
	IsTruncated bool     `xml:"IsTruncated"`
	Contents    []Object `xml:"Contents"`
}

type Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// ListBucketsResult — структура для XML-ответа S3 (список всех бакетов)
type ListBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets []Bucket `xml:"Buckets>Bucket"`
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// HandleListObjectsV2 обрабатывает GET /bucket?list-type=2
func (h *ObjectHandler) HandleListObjectsV2(w http.ResponseWriter, r *http.Request, bucket string) {
	var contents []Object

	if h.repo != nil {
		// Берем tenant_id из контекста или используем дефолтный
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

		objects, err := h.repo.ListObjects(r.Context(), b.ID)
		if err != nil {
			WriteError(w, "InternalError", "Failed to list objects: "+err.Error(), http.StatusInternalServerError)
			return
		}

		for _, obj := range objects {
			contents = append(contents, Object{
				Key:          obj.Key,
				LastModified: obj.CreatedAt.Format(time.RFC3339),
				ETag:         obj.ETag,
				Size:         obj.Size,
				StorageClass: "STANDARD",
			})
		}
	} else {
		// Фолбэк на файловую систему
		keys, err := h.store.List(bucket)
		if err != nil {
			WriteError(w, "InternalError", "Failed to list objects: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for _, key := range keys {
			contents = append(contents, Object{
				Key:          key,
				LastModified: time.Now().UTC().Format(time.RFC3339),
				Size:         0,
				StorageClass: "STANDARD",
			})
		}
	}

	result := ListBucketResult{
		Name:        bucket,
		KeyCount:    len(contents),
		MaxKeys:     1000,
		IsTruncated: false,
		Contents:    contents,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	if err := xml.NewEncoder(w).Encode(result); err != nil {
		// Заголовки уже отправлены, клиенту ошибку не сообщить.
		// Логируем, чтобы не игнорировать молча.
		fmt.Fprintf(os.Stderr, "s3api: encode ListBucketResult: %v\n", err)
	}
}

// HandleListBuckets обрабатывает GET / (список всех бакетов)
func (h *ObjectHandler) HandleListBuckets(w http.ResponseWriter, r *http.Request) {
	var buckets []*metadata.Bucket

	if h.repo != nil {
		tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
		if val := r.Context().Value(tenantIDKey); val != nil {
			if s, ok := val.(string); ok {
				tenantID = s
			}
		}

		var err error
		buckets, err = h.repo.ListBuckets(r.Context(), tenantID)
		if err != nil {
			WriteError(w, "InternalError", "Failed to list buckets: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var bucketsXML []Bucket
	for _, b := range buckets {
		bucketsXML = append(bucketsXML, Bucket{
			Name:         b.Name,
			CreationDate: b.CreatedAt.Format(time.RFC3339),
		})
	}

	result := ListBucketsResult{
		Owner: Owner{
			ID:          "test-owner",
			DisplayName: "test-owner",
		},
		Buckets: bucketsXML,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	if err := xml.NewEncoder(w).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "s3api: encode ListBucketsResult: %v\n", err)
	}
}

// HandleCreateBucket обрабатывает PUT /bucket (создание бакета)
func (h *ObjectHandler) HandleCreateBucket(w http.ResponseWriter, r *http.Request, bucketName string) {
	if h.repo == nil {
		WriteError(w, "InternalError", "Metadata DB not configured", http.StatusInternalServerError)
		return
	}

	tenantID := "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b"
	if val := r.Context().Value(tenantIDKey); val != nil {
		if s, ok := val.(string); ok {
			tenantID = s
		}
	}

	_, err := h.repo.CreateBucket(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == metadata.ErrAlreadyExists {
			WriteError(w, "BucketAlreadyExists", "The requested bucket name is not available.", http.StatusConflict)
			return
		}
		WriteError(w, "InternalError", "Failed to create bucket: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
