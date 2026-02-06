package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	// MaxLineBytes is the maximum size of a single JSON line accepted by the JSONL protocol.
	// 1 MiB is enough for fairly large base64 pixel payloads while keeping memory bounded.
	MaxLineBytes = 1 << 20
)

// ReadLine reads a single '\n'-terminated line from r with a hard size limit.
func ReadLine(r *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
		buf.Write(chunk)
		if buf.Len() > MaxLineBytes {
			return nil, fmt.Errorf("jsonl line too large (> %d bytes)", MaxLineBytes)
		}
		if !isPrefix {
			break
		}
	}
	return buf.Bytes(), nil
}

func EncodeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func DecodeRequestLine(line []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, err
	}
	return &req, nil
}
