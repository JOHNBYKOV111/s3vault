package s3api

import (
	"encoding/xml"
	"net/http"
)

// S3Error — структура для XML-ответа об ошибке
type S3Error struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// WriteError отправляет S3-совместимую ошибку в формате XML
func WriteError(w http.ResponseWriter, code string, message string, status int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)

	errResp := S3Error{
		Code:    code,
		Message: message,
	}

	xml.NewEncoder(w).Encode(errResp)
}
