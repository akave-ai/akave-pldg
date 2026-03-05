package chunk

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/akave-ai/akavelog/internal/storage"
)

// chunkPayload is the JSON shape we write to O3 (gzipped).
type chunkPayload struct {
	Labels  map[string]string `json:"labels"`
	Entries []entryPayload    `json:"entries"`
}

type entryPayload struct {
	TsNs int64  `json:"ts_ns"`
	Line string `json:"line"`
}

// StreamID returns a stable ID for the given labels (for use in chunk keys).
func StreamID(labels map[string]string) string {
	if len(labels) == 0 {
		return "default"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(b.String()))
	return fmt.Sprintf("%016x", h.Sum64())
}

// O3Store implements chunk.Store by writing gzipped JSON to O3.
type O3Store struct {
	client *storage.O3Client
}

// NewO3Store returns a Store that writes chunks to O3.
func NewO3Store(client *storage.O3Client) *O3Store {
	return &O3Store{client: client}
}

// Put encodes each chunk as gzipped JSON and uploads to O3.
func (s *O3Store) Put(ctx context.Context, chunks []Chunk) error {
	if s.client == nil {
		return fmt.Errorf("o3 client not configured")
	}
	for _, ch := range chunks {
		key := KeyForChunk(ch.Tenant, StreamID(ch.Labels), ch.FromNs, ch.ToNs)
		payload := chunkPayload{Labels: ch.Labels, Entries: make([]entryPayload, len(ch.Entries))}
		for i, e := range ch.Entries {
			payload.Entries[i] = entryPayload{TsNs: e.TsNs, Line: e.Line}
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal chunk: %w", err)
		}
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(raw); err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("gzip close: %w", err)
		}
		if err := s.client.PutObject(ctx, key, buf.Bytes(), "application/gzip"); err != nil {
			return fmt.Errorf("put object %s: %w", key, err)
		}
	}
	return nil
}
