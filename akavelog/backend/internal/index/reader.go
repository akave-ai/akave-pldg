package index

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/akave-ai/akavelog/internal/storage"
)

// Reader reads the Progress index from O3 to resolve chunk keys for queries.
type Reader struct {
	client *storage.O3Client
}

// NewReader creates an index reader that reads from O3.
func NewReader(client *storage.O3Client) *Reader {
	return &Reader{client: client}
}

// ChunkRef is a chunk key and its time range (for query results).
type ChunkRef struct {
	ChunkKey string
	FromNs   int64
	ToNs     int64
}

// ListChunkKeys returns chunk keys that may contain logs for the given tenant, streamID, and time range.
// fromNs/toNs are inclusive bounds; any index entry that overlaps [fromNs, toNs] is returned.
func (r *Reader) ListChunkKeys(ctx context.Context, tenant, streamID string, fromNs, toNs int64) ([]ChunkRef, error) {
	if r.client == nil {
		return nil, nil
	}
	if tenant == "" {
		tenant = "default"
	}

	// List index objects for this tenant for the date range that covers [fromNs, toNs].
	fromTime := time.Unix(0, fromNs).UTC()
	toTime := time.Unix(0, toNs).UTC()
	if toNs <= 0 {
		toTime = time.Now().UTC()
	}
	var keys []string
	for d := time.Date(fromTime.Year(), fromTime.Month(), fromTime.Day(), 0, 0, 0, 0, time.UTC); !d.After(toTime); d = d.AddDate(0, 0, 1) {
		prefix := PrefixForTenantDate(tenant, d)
		list, err := r.client.ListObjects(ctx, prefix)
		if err != nil {
			return nil, err
		}
		for _, o := range list {
			if strings.HasSuffix(o.Key, ".ndjson") {
				keys = append(keys, o.Key)
			}
		}
	}

	var refs []ChunkRef
	seen := make(map[string]bool)
	for _, key := range keys {
		raw, err := r.client.GetObject(ctx, key)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		for scanner.Scan() {
			var e Entry
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				continue
			}
			if e.StreamID != streamID && streamID != "" {
				continue
			}
			// Overlap: entry [e.FromNs, e.ToNs] overlaps query [fromNs, toNs]
			if e.ToNs < fromNs || e.FromNs > toNs {
				continue
			}
			if seen[e.ChunkKey] {
				continue
			}
			seen[e.ChunkKey] = true
			refs = append(refs, ChunkRef{ChunkKey: e.ChunkKey, FromNs: e.FromNs, ToNs: e.ToNs})
		}
	}
	return refs, nil
}
