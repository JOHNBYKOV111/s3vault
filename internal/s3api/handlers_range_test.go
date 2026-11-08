package s3api

import (
	"testing"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		fileSize  int64
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{"no header", "", 100, 0, 0, false},
		{"valid range", "bytes=0-10", 100, 0, 10, false},
		{"from middle", "bytes=50-60", 100, 50, 60, false},
		{"open end", "bytes=50-", 100, 50, 99, false},
		{"end bigger than file", "bytes=50-150", 100, 50, 99, false},
		{"invalid header", "invalid", 100, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeReq, err := ParseRange(tt.header, tt.fileSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.header == "" {
				if rangeReq != nil {
					t.Errorf("expected nil range, got %v", rangeReq)
				}
				return
			}
			if rangeReq.Start != tt.wantStart || rangeReq.End != tt.wantEnd {
				t.Errorf("expected %d-%d, got %d-%d", tt.wantStart, tt.wantEnd, rangeReq.Start, rangeReq.End)
			}
		})
	}
}
