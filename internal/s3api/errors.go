package s3api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
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

	if err := xml.NewEncoder(w).Encode(errResp); err != nil {
		// Заголовки уже отправлены, клиенту ошибку не сообщить.
		fmt.Fprintf(os.Stderr, "s3api: encode S3Error: %v\n", err)
	}
}
