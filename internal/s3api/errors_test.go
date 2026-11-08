package s3api

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, "NoSuchKey", "The specified key does not exist.", 404)

	if rr.Code != 404 {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var s3err S3Error
	if err := xml.NewDecoder(strings.NewReader(rr.Body.String())).Decode(&s3err); err != nil {
		t.Fatalf("failed to decode XML: %v", err)
	}

	if s3err.Code != "NoSuchKey" {
		t.Errorf("expected Code NoSuchKey, got %s", s3err.Code)
	}
}
