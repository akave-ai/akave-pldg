package push

import (
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	contentTypeHeader = "Content-Type"
	contentEncHeader  = "Content-Encoding"
	applicationJSON   = "application/json"
)

// ErrRequestBodyTooLarge is returned when the body (compressed or decompressed) exceeds the limit.
var ErrRequestBodyTooLarge = errors.New("request body too large")

// ParseRequestBody reads the HTTP request body and returns a PushRequest.
// It respects Content-Encoding (gzip, deflate) and Content-Type (application/json).
// maxRecvMsgSize limits the compressed body size; maxDecompressedSize limits after decompression.
// Snappy/protobuf is not supported and returns an error.
func ParseRequestBody(r *http.Request, maxRecvMsgSize int, maxDecompressedSize int64) (*PushRequest, error) {
	body := r.Body
	if body == nil {
		return nil, fmt.Errorf("missing body")
	}

	// Apply compressed size limit
	var reader io.Reader = body
	if maxRecvMsgSize > 0 {
		reader = io.LimitReader(reader, int64(maxRecvMsgSize)+1)
	}

	contentEncoding := strings.TrimSpace(strings.ToLower(r.Header.Get(contentEncHeader)))
	switch contentEncoding {
	case "":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
		if maxDecompressedSize > 0 {
			reader = io.LimitReader(reader, maxDecompressedSize+1)
		}
	case "deflate":
		fl := flate.NewReader(reader)
		defer fl.Close()
		reader = fl
		if maxDecompressedSize > 0 {
			reader = io.LimitReader(reader, maxDecompressedSize+1)
		}
	default:
		return nil, fmt.Errorf("Content-Encoding %q not supported", contentEncoding)
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if contentEncoding != "" {
		if maxDecompressedSize > 0 && int64(len(raw)) > maxDecompressedSize {
			return nil, ErrRequestBodyTooLarge
		}
	} else {
		if maxRecvMsgSize > 0 && len(raw) > maxRecvMsgSize {
			return nil, ErrRequestBodyTooLarge
		}
	}

	contentType := r.Header.Get(contentTypeHeader)
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		contentType = ""
	}

	switch contentType {
	case applicationJSON:
		req, err := ParsePushRequest(raw)
		if err != nil {
			return nil, err
		}
		return req, nil
	default:
		// When no Content-Type or application/x-protobuf: Snappy protobuf not implemented for Akavelog
		return nil, fmt.Errorf("only application/json is supported; Snappy protobuf not implemented")
	}
}
