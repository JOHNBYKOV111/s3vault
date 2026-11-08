package blob

import (
	"io"
)

type Chunker struct {
	reader    io.Reader
	chunkSize int
	buf       []byte
}

func NewChunker(r io.Reader, chunkSize int) *Chunker {
	return &Chunker{
		reader:    r,
		chunkSize: chunkSize,
		buf:       make([]byte, chunkSize),
	}
}

func (c *Chunker) Next() ([]byte, error) {
	n, err := io.ReadFull(c.reader, c.buf)
	if err == io.ErrUnexpectedEOF {
		return c.buf[:n], nil
	}
	if err == io.EOF {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	return c.buf, nil
}
