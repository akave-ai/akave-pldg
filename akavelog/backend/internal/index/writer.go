package index

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/storage"
	"github.com/google/uuid"
)

const (
	defaultIndexBatchSize   = 100
	defaultIndexFlushInterval = 30 * time.Second
)

// Writer records chunk references to the Progress index (not TSDB).
// Entries are buffered and written as NDJSON batches to O3.
type Writer interface {
	// IndexChunk records one chunk for the given stream and time range.
	IndexChunk(ctx context.Context, tenant, streamID string, fromNs, toNs int64, chunkKey string)
	// Flush writes any buffered entries to O3. Safe to call concurrently with IndexChunk.
	Flush(ctx context.Context) error
	// Stop flushes and stops the background flush loop.
	Stop()
}

// O3Writer implements Writer by buffering entries and writing NDJSON batches to O3.
type O3Writer struct {
	client   *storage.O3Client
	batchSize int
	interval  time.Duration
	mu       sync.Mutex
	buf      []Entry
	stop     chan struct{}
	done     chan struct{}
}

// O3WriterConfig configures the O3 index writer.
type O3WriterConfig struct {
	BatchSize int           // flush when buffer has this many entries (default 100)
	Interval  time.Duration // flush at least this often (default 30s)
}

// DefaultO3WriterConfig returns default config.
func DefaultO3WriterConfig() O3WriterConfig {
	return O3WriterConfig{
		BatchSize: defaultIndexBatchSize,
		Interval:  defaultIndexFlushInterval,
	}
}

// NewO3Writer creates an index writer that writes to O3. Start background flush with Run(ctx).
func NewO3Writer(client *storage.O3Client, cfg O3WriterConfig) *O3Writer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultIndexBatchSize
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultIndexFlushInterval
	}
	return &O3Writer{
		client:    client,
		batchSize: cfg.BatchSize,
		interval:  cfg.Interval,
		buf:       make([]Entry, 0, cfg.BatchSize*2),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// IndexChunk adds one chunk reference to the buffer; may trigger flush if batch is full.
func (w *O3Writer) IndexChunk(ctx context.Context, tenant, streamID string, fromNs, toNs int64, chunkKey string) {
	if w.client == nil {
		return
	}
	w.mu.Lock()
	w.buf = append(w.buf, Entry{
		Tenant:   tenant,
		StreamID: streamID,
		FromNs:   fromNs,
		ToNs:     toNs,
		ChunkKey: chunkKey,
	})
	shouldFlush := len(w.buf) >= w.batchSize
	w.mu.Unlock()
	if shouldFlush {
		_ = w.Flush(ctx)
	}
}

// Flush writes buffered entries as NDJSON object(s) to O3 (one per tenant) and clears the buffer.
func (w *O3Writer) Flush(ctx context.Context) error {
	if w.client == nil {
		return nil
	}
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return nil
	}
	snapshot := make([]Entry, len(w.buf))
	copy(snapshot, w.buf)
	w.buf = w.buf[:0]
	w.mu.Unlock()

	// Group by tenant so each object key is index/<tenant>/<date>/<id>.ndjson
	byTenant := make(map[string][]Entry)
	for i := range snapshot {
		t := snapshot[i].Tenant
		if t == "" {
			t = "default"
		}
		byTenant[t] = append(byTenant[t], snapshot[i])
	}
	now := time.Now().UTC()
	for tenant, entries := range byTenant {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		for i := range entries {
			if err := enc.Encode(&entries[i]); err != nil {
				return err
			}
		}
		key := KeyForIndexBatch(tenant, now, uuid.New().String())
		if err := w.client.PutObject(ctx, key, b.Bytes(), "application/x-ndjson"); err != nil {
			log.Printf("[index] write %s: %v", key, err)
			return err
		}
		log.Printf("[index] wrote %s (%d entries)", key, len(entries))
	}
	return nil
}

// Run starts the background flush loop. Call Stop() to shut down.
func (w *O3Writer) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			_ = w.Flush(ctx)
		}
	}
}

// Stop stops the flush loop and flushes remaining entries.
func (w *O3Writer) Stop() {
	close(w.stop)
	<-w.done
	_ = w.Flush(context.Background())
}
