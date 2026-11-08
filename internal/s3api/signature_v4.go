package s3api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ValidateSignatureV4 проверяет полную подпись AWS Signature V4.
// Возвращает nil, если подпись верна, иначе — ошибку.
func ValidateSignatureV4(r *http.Request, secretKey, region string) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return errors.New("missing Authorization header")
	}

	// Парсим заголовок Authorization
	// Формат: AWS4-HMAC-SHA256 Credential=.../..., SignedHeaders=..., Signature=...
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "AWS4-HMAC-SHA256" {
		return errors.New("unsupported authorization algorithm")
	}

	// Разбираем поля
	var credential, signedHeaders, signature string
	for _, field := range strings.Split(parts[1], ", ") {
		if strings.HasPrefix(field, "Credential=") {
			credential = strings.TrimPrefix(field, "Credential=")
		} else if strings.HasPrefix(field, "SignedHeaders=") {
			signedHeaders = strings.TrimPrefix(field, "SignedHeaders=")
		} else if strings.HasPrefix(field, "Signature=") {
			signature = strings.TrimPrefix(field, "Signature=")
		}
	}

	if credential == "" || signedHeaders == "" || signature == "" {
		return errors.New("missing required fields in Authorization header")
	}

	// Проверяем дату (x-amz-date) — защита от replay-атак
	amzDate := r.Header.Get("x-amz-date")
	if amzDate == "" {
		return errors.New("missing x-amz-date header")
	}

	reqTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return fmt.Errorf("invalid x-amz-date format: %w", err)
	}

	// Подпись действительна 15 минут
	if time.Since(reqTime) > 15*time.Minute {
		return errors.New("signature expired")
	}

	// 1. Создаем канонический запрос
	canonicalRequest, err := buildCanonicalRequest(r, signedHeaders)
	if err != nil {
		return fmt.Errorf("build canonical request: %w", err)
	}

	// 2. Вычисляем строку для подписи
	stringToSign := buildStringToSign(amzDate, credential, canonicalRequest)

	// 3. Вычисляем подпись
	expectedSignature := calculateSignature(stringToSign, secretKey, region, amzDate)

	// 4. Сравниваем (constant-time)
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return errors.New("signature mismatch")
	}

	return nil
}

// buildCanonicalRequest создает канонический запрос (Canonical Request).
func buildCanonicalRequest(r *http.Request, signedHeaders string) (string, error) {
	// 1. HTTP-метод
	method := r.Method

	// 2. Канонический URI (path)
	canonicalURI := r.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// 3. Каноническая строка запроса (query string)
	canonicalQuery := buildCanonicalQuery(r.URL.Query())

	// 4. Канонические заголовки
	canonicalHeaders, err := buildCanonicalHeaders(r, signedHeaders)
	if err != nil {
		return "", err
	}

	// 5. Хэш тела (для простоты — SHA-256 от пустого тела, или x-amz-content-sha256)
	payloadHash := r.Header.Get("x-amz-content-sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	// Собираем канонический запрос
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		"",
		signedHeaders,
		payloadHash,
	}, "\n")

	return canonicalRequest, nil
}

// buildCanonicalQuery создает каноническую строку запроса.
func buildCanonicalQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}

	// Сортируем ключи
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Собираем "key=value" пары (с URL-кодированием)
	var parts []string
	for _, k := range keys {
		values := query[k]
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}

	return strings.Join(parts, "&")
}

// buildCanonicalHeaders создает канонические заголовки.
func buildCanonicalHeaders(r *http.Request, signedHeaders string) (string, error) {
	headers := strings.Split(signedHeaders, ";")
	var lines []string
	for _, h := range headers {
		value := r.Header.Get(h)
		if value == "" && h == "host" {
			value = r.Host
		}
		lines = append(lines, strings.ToLower(h)+":"+strings.TrimSpace(value))
	}
	return strings.Join(lines, "\n"), nil
}

// buildStringToSign создает строку для подписи.
func buildStringToSign(amzDate, credential, canonicalRequest string) string {
	// Хэш канонического запроса
	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	canonicalRequestHashHex := hex.EncodeToString(canonicalRequestHash[:])

	// Извлекаем scope из credential (date/region/service/aws4_request)
	credentialParts := strings.Split(credential, "/")
	scope := strings.Join(credentialParts[1:], "/")

	return strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		canonicalRequestHashHex,
	}, "\n")
}

// calculateSignature вычисляет HMAC-SHA256 подпись.
func calculateSignature(stringToSign, secretKey, region, amzDate string) string {
	// 1. DateKey = HMAC("AWS4" + secretKey, date)
	date := amzDate[:8] // YYYYMMDD
	dateKey := hmacSHA256([]byte("AWS4"+secretKey), []byte(date))

	// 2. RegionKey = HMAC(DateKey, region)
	regionKey := hmacSHA256(dateKey, []byte(region))

	// 3. ServiceKey = HMAC(RegionKey, "s3")
	serviceKey := hmacSHA256(regionKey, []byte("s3"))

	// 4. SigningKey = HMAC(ServiceKey, "aws4_request")
	signingKey := hmacSHA256(serviceKey, []byte("aws4_request"))

	// 5. Signature = HMAC(SigningKey, stringToSign)
	signature := hmacSHA256(signingKey, []byte(stringToSign))

	return hex.EncodeToString(signature)
}

// hmacSHA256 вычисляет HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
