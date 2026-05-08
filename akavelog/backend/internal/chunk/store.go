package chunk

import (
	"context"
	"fmt"
	"path"
)

// Entry is a single log line for storage (timestamp ns + line).
type Entry struct {
	TsNs int64
	Line string
}

// Chunk is a closed chunk: stream labels and entries, with time bounds for keying.
type Chunk struct {
	Tenant  string
	Labels  map[string]string
	Entries []Entry
	FromNs  int64
	ToNs    int64
}

// Store writes chunks to durable storage (e.g. O3).
type Store interface {
	Put(ctx context.Context, chunks []Chunk) error
}

// KeyForChunk returns object key for a chunk: chunks/<tenant>/<streamID>/<from>_<to>.json.gz
func KeyForChunk(tenant string, streamID string, fromNs, toNs int64) string {
	if tenant == "" {
		tenant = "default"
	}
	return path.Join("chunks", tenant, streamID, fmtNs(fromNs)+"_"+fmtNs(toNs)+".json.gz")
}

func fmtNs(ns int64) string {
	return fmt.Sprintf("%d", ns)
}
