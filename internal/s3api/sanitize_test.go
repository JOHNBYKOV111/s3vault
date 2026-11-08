package s3api

import (
	"testing"
)

func TestParseS3Path(t *testing.T) {
	tests := []struct {
		path       string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{"/mybucket/photo.jpg", "mybucket", "photo.jpg", false},
		{"/mybucket/folder/file.txt", "mybucket", "folder/file.txt", false},
		{"/mybucket", "mybucket", "", false},
		{"/", "", "", true},              // Ошибка: нет бакета
		{"", "", "", true},               // Ошибка: пустой путь
		{"/../etc/passwd", "", "", true}, // Ошибка: path traversal (защита)
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			bucket, key, err := ParseS3Path(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseS3Path() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if bucket != tt.wantBucket {
				t.Errorf("ParseS3Path() bucket = %v, want %v", bucket, tt.wantBucket)
			}
			if key != tt.wantKey {
				t.Errorf("ParseS3Path() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func FuzzParseS3Path(f *testing.F) {
	f.Add("/mybucket/photo.jpg")
	f.Add("/../etc/passwd")
	f.Add("")

	f.Fuzz(func(t *testing.T, path string) {
		bucket, key, err := ParseS3Path(path)

		// Если ошибки нет, значит бакет не должен быть пустым
		if err == nil && bucket == "" {
			t.Errorf("bucket is empty but no error for path: %s", path)
		}

		// Если ошибки нет, ключ не должен начинаться со слэша
		if err == nil && len(key) > 0 && key[0] == '/' {
			t.Errorf("key starts with slash: %q for path: %q", key, path)
		}
	})
}
