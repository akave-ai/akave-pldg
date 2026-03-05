package ingester

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/index"
	"github.com/akave-ai/akavelog/internal/push"
)

const defaultTenant = "default"
const flushQueueSize = 1024
const sweepInterval = 15 * time.Second

// Ingester receives push requests, appends to streams/chunks in memory, and flushes closed chunks to the store.
type Ingester struct {
	config      StreamConfig
	store       chunk.Store
	indexWriter index.Writer // optional; records chunk refs to Progress index
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
// indexWriter is optional; when set, chunk refs are written to the Progress index (not TSDB).
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
			ch := chunk.Chunk{
				Tenant:  op.tenant,
				Labels:  op.labels,
				Entries: make([]chunk.Entry, len(op.desc.entries)),
				FromNs:  op.desc.fromNs,
				ToNs:    op.desc.toNs,
			}
			copy(ch.Entries, op.desc.entries)
			if err := i.store.Put(ctx, []chunk.Chunk{ch}); err != nil {
				log.Printf("[ingester] flush put: %v", err)
				continue
			}
			streamID := chunk.StreamID(ch.Labels)
			key := chunk.KeyForChunk(ch.Tenant, streamID, ch.FromNs, ch.ToNs)
			log.Printf("[ingester] flushed %s (%d entries)", key, len(ch.Entries))
			if i.indexWriter != nil {
				i.indexWriter.IndexChunk(ctx, ch.Tenant, streamID, ch.FromNs, ch.ToNs, key)
			}
			if i.onFlush != nil {
				i.onFlush(len(ch.Entries), key)
			}
		}
	}
}
