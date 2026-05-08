package ingester

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/index"
	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
	"github.com/akave-ai/akavelog/internal/push"
)

const defaultTenant = "default"
const flushQueueSize = 1024
const sweepInterval = 15 * time.Second

// DBIndexer is the interface the ingester uses to write metadata to PostgreSQL after
// each successful O3 upload. Implemented by *repository.LogBatchRepository.
// Defined here (not in repository) to keep the ingester free of DB import cycles.
type DBIndexer interface {
	Insert(ctx context.Context, p logbatches.InsertParams) error
}

// Ingester receives push requests, appends to streams/chunks in memory, and flushes closed chunks to the store.
type Ingester struct {
	config      StreamConfig
	store       chunk.Store
	indexWriter index.Writer // optional; records chunk refs to O3 Progress index
	dbIndexer   DBIndexer    // optional; writes metadata row to PostgreSQL (Phase 4)
	mu          sync.RWMutex
	instances   map[string]*Instance
	flushQueue  chan *flushOp
	stop        chan struct{}
	done        chan struct{}
	onFlush     func(count int, key string)
}

// flushOp is a closed chunk ready to write to the store.
type flushOp struct {
	tenant string
	labels map[string]string
	desc   *chunkDesc
}

// NewIngester creates an ingester that writes chunks to the given store.
// indexWriter is optional; when set, chunk refs are written to the O3 Progress index.
// dbIndexer  is optional; when set, metadata rows are written to PostgreSQL.
func NewIngester(store chunk.Store, config StreamConfig, indexWriter index.Writer) *Ingester {
	if store == nil {
		panic("store is required")
	}
	if config.MaxChunkEntries <= 0 {
		config.MaxChunkEntries = DefaultStreamConfig().MaxChunkEntries
	}
	if config.ChunkIdlePeriod <= 0 {
		config.ChunkIdlePeriod = DefaultStreamConfig().ChunkIdlePeriod
	}
	if config.MaxChunkAge <= 0 {
		config.MaxChunkAge = DefaultStreamConfig().MaxChunkAge
	}
	ing := &Ingester{
		config:      config,
		store:       store,
		indexWriter: indexWriter,
		instances:   make(map[string]*Instance),
		flushQueue:  make(chan *flushOp, flushQueueSize),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	ing.mu.Lock()
	ing.instances[defaultTenant] = newInstance(defaultTenant, config, ing.enqueueFlush)
	ing.mu.Unlock()
	return ing
}

// WithDBIndexer attaches a PostgreSQL metadata indexer (Phase 4).
// Must be called before Start().
func (i *Ingester) WithDBIndexer(db DBIndexer) {
	i.dbIndexer = db
}

// OnFlush sets a callback invoked after each chunk is written (count, object key).
func (i *Ingester) OnFlush(fn func(count int, key string)) {
	i.onFlush = fn
}

// enqueueFlush is called by instance when a chunk is closed.
func (i *Ingester) enqueueFlush(tenant string, labels map[string]string, desc *chunkDesc) {
	op := &flushOp{tenant: tenant, labels: labels, desc: desc}
	select {
	case i.flushQueue <- op:
	default:
		log.Printf("[ingester] flush queue full, dropping chunk")
	}
}

// Push appends the request to the default tenant's instance (single-node; no ring).
func (i *Ingester) Push(ctx context.Context, req *push.PushRequest) {
	i.mu.RLock()
	inst := i.instances[defaultTenant]
	i.mu.RUnlock()
	if inst == nil {
		return
	}
	inst.Push(req)
}

// Start starts the sweep loop (close idle/old chunks) and flush loop (write to store).
func (i *Ingester) Start(ctx context.Context) {
	go i.sweepLoop(ctx)
	go i.flushLoop(ctx)
}

// Stop stops the ingester (sweep and flush loops exit).
func (i *Ingester) Stop() {
	close(i.stop)
	<-i.done
}

func (i *Ingester) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.stop:
			return
		case <-ticker.C:
			now := time.Now()
			i.mu.RLock()
			for _, inst := range i.instances {
				inst.ForEachStream(func(s *Stream) {
					s.FlushCurrentIfNeeded(now)
				})
			}
			i.mu.RUnlock()
		}
	}
}

func (i *Ingester) flushLoop(ctx context.Context) {
	defer close(i.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.stop:
			return
		case op, ok := <-i.flushQueue:
			if !ok {
				return
			}
			if op == nil {
				continue
			}
			i.handleFlush(ctx, op)
		}
	}
}

// handleFlush writes one chunk to O3, then records its metadata in both the O3
// Progress index and the PostgreSQL log_batches table.
func (i *Ingester) handleFlush(ctx context.Context, op *flushOp) {
	ch := chunk.Chunk{
		Tenant:  op.tenant,
		Labels:  op.labels,
		Entries: make([]chunk.Entry, len(op.desc.entries)),
		FromNs:  op.desc.fromNs,
		ToNs:    op.desc.toNs,
	}
	copy(ch.Entries, op.desc.entries)

	// ── 1. Upload chunk to O3 ─────────────────────────────────────────────────
	if err := i.store.Put(ctx, []chunk.Chunk{ch}); err != nil {
		log.Printf("[ingester] flush put: %v", err)
		return
	}

	streamID := chunk.StreamID(ch.Labels)
	key := chunk.KeyForChunk(ch.Tenant, streamID, ch.FromNs, ch.ToNs)
	log.Printf("[ingester] flushed %s (%d entries)", key, len(ch.Entries))

	// ── 2. Record in O3 Progress index (NDJSON) ───────────────────────────────
	if i.indexWriter != nil {
		i.indexWriter.IndexChunk(ctx, ch.Tenant, streamID, ch.FromNs, ch.ToNs, key)
	}

	// ── 3. Record metadata in PostgreSQL log_batches (Phase 4) ───────────────
	if i.dbIndexer != nil {
		params := buildInsertParams(ch, streamID, key)
		if err := i.dbIndexer.Insert(ctx, params); err != nil {
			// Non-fatal: chunk is safe in O3; log and continue.
			log.Printf("[ingester] db index insert failed for %s: %v", key, err)
		}
	}

	// ── 4. Notify server-level status store ───────────────────────────────────
	if i.onFlush != nil {
		i.onFlush(len(ch.Entries), key)
	}
}

// buildInsertParams constructs the PostgreSQL insert payload from a flushed chunk.
// Service is extracted from stream labels using the same priority as the recent-logs UI:
// "job" > "app" > "service" > "akavelog" (fallback).
func buildInsertParams(ch chunk.Chunk, streamID, key string) logbatches.InsertParams {
	service := extractService(ch.Labels)
	levels := extractLevels(ch.Labels)

	return logbatches.InsertParams{
		ProjectID:   nil, // Phase 8 will wire project_id via API-key middleware
		Tenant:      ch.Tenant,
		StreamID:    streamID,
		Service:     service,
		TsStart:     time.Unix(0, ch.FromNs).UTC(),
		TsEnd:       time.Unix(0, ch.ToNs).UTC(),
		Levels:      levels,
		Tags:        ch.Labels,
		O3ObjectKey: key,
		EntryCount:  len(ch.Entries),
	}
}

// extractService mirrors the labelsToLogEntry heuristic in server.go so that
// the service stored in log_batches is consistent with what the UI shows.
func extractService(labels map[string]string) string {
	if labels == nil {
		return "akavelog"
	}
	for _, key := range []string{"job", "app", "service"} {
		if v := labels[key]; v != "" {
			return v
		}
	}
	return "akavelog"
}

// extractLevels returns the "level" label as a single-element slice if present,
// or an empty slice. Phase 5 can enrich this by scanning entry lines.
func extractLevels(labels map[string]string) []string {
	if labels == nil {
		return []string{}
	}
	if l := labels["level"]; l != "" {
		return []string{l}
	}
	return []string{}
}
