package wasijob

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
)

func EncodeResult(data []byte) (string, error) {
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	output := base64.StdEncoding.EncodeToString(compressed.Bytes())
	if len(output)+1 > 2048 {
		return "", fmt.Errorf("compressed result exceeds Bacalhau's 2 KiB stdout limit")
	}
	return output, nil
}

func DecodeResult(encoded []byte) ([]byte, error) {
	decoded := base64.NewDecoder(base64.StdEncoding, bytes.NewReader(encoded))
	reader, err := gzip.NewReader(decoded)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	const maxResult = 8 << 20
	output, err := io.ReadAll(io.LimitReader(reader, maxResult+1))
	if len(output) > maxResult {
		return nil, fmt.Errorf("decoded worker result exceeds %d bytes", maxResult)
	}
	return output, err
}
