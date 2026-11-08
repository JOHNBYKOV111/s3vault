package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

func WriteRecord(w io.Writer, payload []byte) error {
	length := uint32(len(payload))
	crc := crc32.ChecksumIEEE(payload)

	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, crc); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadRecord(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	var expectedCRC uint32
	if err := binary.Read(r, binary.LittleEndian, &expectedCRC); err != nil {
		return nil, err
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	actualCRC := crc32.ChecksumIEEE(payload)
	if actualCRC != expectedCRC {
		return nil, errors.New("CRC mismatch")
	}

	return payload, nil
}
