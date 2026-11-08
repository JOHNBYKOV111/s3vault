package s3api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// signRequest подписывает запрос (для теста).
func signRequest(req *http.Request, secretKey, region string) {
	amzDate := req.Header.Get("x-amz-date")
	date := amzDate[:8]

	// 1. Канонический запрос (через strings.Join, чтобы не путаться с аргументами)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		"",
		"host:" + req.Host + "\nx-amz-date:" + amzDate,
		"",
		"host;x-amz-date",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	// 2. Строка для подписи
	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", date, region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate, scope, hex.EncodeToString(canonicalRequestHash[:]))

	// 3. Вычисляем подпись (используем hmacSHA256 из signature_v4.go)
	dateKey := hmacSHA256([]byte("AWS4"+secretKey), []byte(date))
	regionKey := hmacSHA256(dateKey, []byte(region))
	serviceKey := hmacSHA256(regionKey, []byte("s3"))
	signingKey := hmacSHA256(serviceKey, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 4. Устанавливаем заголовок Authorization
	credential := fmt.Sprintf("test/%s/%s/s3/aws4_request", date, region)
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s, SignedHeaders=host;x-amz-date, Signature=%s",
		credential, signature,
	))
}

func TestSignatureV4(t *testing.T) {
	secretKey := "test-secret"
	region := "us-east-1"

	handler := func(w http.ResponseWriter, r *http.Request) {
		if err := ValidateSignatureV4(r, secretKey, region); err != nil {
			WriteError(w, "AccessDenied", err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}

	// 1. Запрос без подписи (403)
	req := httptest.NewRequest("GET", "/mybucket/file.txt", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unsigned request, got %d", rr.Code)
	}

	// 2. Запрос с корректной подписью (200)
	req = httptest.NewRequest("GET", "/mybucket/file.txt", nil)
	req.Host = "localhost:9000"
	req.Header.Set("x-amz-date", time.Now().UTC().Format("20060102T150405Z"))
	signRequest(req, secretKey, region)
	rr = httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for valid signature, got %d", rr.Code)
	}

	// 3. Запрос с неверной подписью (403)
	req = httptest.NewRequest("GET", "/mybucket/file.txt", nil)
	req.Host = "localhost:9000"
	req.Header.Set("x-amz-date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=test/20261106/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=wrong")
	rr = httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong signature, got %d", rr.Code)
	}

	// 4. Запрос с просроченной датой (403)
	req = httptest.NewRequest("GET", "/mybucket/file.txt", nil)
	req.Host = "localhost:9000"
	oldDate := time.Now().Add(-1 * time.Hour).UTC().Format("20060102T150405Z")
	req.Header.Set("x-amz-date", oldDate)
	signRequest(req, secretKey, region)
	rr = httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for expired signature, got %d", rr.Code)
	}
}
