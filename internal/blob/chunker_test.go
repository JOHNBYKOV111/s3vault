package blob

import (
	"bytes"
	"io"
	"runtime"
	"testing"
)

func TestChunker_Correctness(t *testing.T) {
	data := []byte("Hello, S3 Chunker! This is a test of chunking.")
	chunkSize := 10

	reader := bytes.NewReader(data)
	chunker := NewChunker(reader, chunkSize)

	var result []byte
	for {
		chunk, err := chunker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result = append(result, chunk...)
	}

	if !bytes.Equal(data, result) {
		t.Errorf("expected %s, got %s", data, result)
	}
}

func TestChunker_Memory(t *testing.T) {
	reader := io.LimitReader(zeroReader{}, 100*1024*1024)
	chunker := NewChunker(reader, 4*1024*1024)

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	for {
		_, err := chunker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	diff := int64(m2.Alloc) - int64(m1.Alloc)
	if diff > 20*1024*1024 {
		t.Errorf("memory grew by %d bytes (more than 20 MB)", diff)
	}
}

type zeroReader struct{}

func (z zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
