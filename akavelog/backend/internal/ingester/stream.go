package ingester

import (
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/push"
)

// chunkDesc holds in-memory entries for one chunk (open or closed pending flush).
type chunkDesc struct {
	entries    []chunk.Entry
	fromNs     int64
	toNs       int64
	createdAt  time.Time
	lastAppend time.Time
	closed     bool
}

// Stream holds labels and chunks for one stream (keyed by labels).
type Stream struct {
	labels  map[string]string
	mu      sync.Mutex
	current *chunkDesc
	pending []*chunkDesc // closed chunks waiting for flush
	config  StreamConfig
	onChunk func(*chunkDesc) // enqueue for flush when closed
}

// StreamConfig controls when chunks are closed and flushed.
type StreamConfig struct {
	ChunkIdlePeriod time.Duration // close after no appends for this long (e.g. 30m)
	MaxChunkAge     time.Duration // close after this long since first append (e.g. 2h)
	MaxChunkEntries int           // close when entry count reaches this (e.g. 10000)
}

// DefaultStreamConfig returns default chunk limits.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		ChunkIdlePeriod: 30 * time.Minute,
		MaxChunkAge:     2 * time.Hour,
		MaxChunkEntries: 10000,
	}
}

// newStream creates a stream. onChunk is called when a chunk is closed (to enqueue flush).
func newStream(labels map[string]string, config StreamConfig, onChunk func(*chunkDesc)) *Stream {
	return &Stream{labels: labels, config: config, onChunk: onChunk}
}

// Push appends entries to the current chunk (creating one if needed). May close current chunk if full.
func (s *Stream) Push(entries []push.Entry) {
	if len(entries) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range entries {
		if s.current == nil {
			from := e.TsNs
			if from == 0 {
				from = time.Now().UnixNano()
			}
			s.current = &chunkDesc{
				entries:    make([]chunk.Entry, 0, 256),
				fromNs:     from,
				toNs:       from,
				createdAt:  time.Now(),
				lastAppend: time.Now(),
			}
		}
		s.current.entries = append(s.current.entries, chunk.Entry{TsNs: e.TsNs, Line: e.Line})
		if e.TsNs > 0 {
			if s.current.fromNs == 0 || e.TsNs < s.current.fromNs {
				s.current.fromNs = e.TsNs
			}
			if e.TsNs > s.current.toNs {
				s.current.toNs = e.TsNs
			}
		}
		s.current.lastAppend = time.Now()

		if s.config.MaxChunkEntries > 0 && len(s.current.entries) >= s.config.MaxChunkEntries {
			s.closeCurrentLocked()
		}
	}
}

// closeCurrentLocked closes the current chunk and enqueues it for flush. Caller must hold mu.
func (s *Stream) closeCurrentLocked() {
	if s.current == nil || len(s.current.entries) == 0 {
		return
	}
	s.current.closed = true
	s.pending = append(s.pending, s.current)
	if s.onChunk != nil {
		s.onChunk(s.current)
	}
	s.current = nil
}

// ShouldFlush returns true if the current chunk should be closed (idle or max age).
func (s *Stream) ShouldFlush(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || len(s.current.entries) == 0 {
		return false
	}
	if s.config.ChunkIdlePeriod > 0 && now.Sub(s.current.lastAppend) >= s.config.ChunkIdlePeriod {
		return true
	}
	if s.config.MaxChunkAge > 0 && now.Sub(s.current.createdAt) >= s.config.MaxChunkAge {
		return true
	}
	return false
}

// FlushCurrentIfNeeded closes the current chunk if ShouldFlush. Call from sweep.
func (s *Stream) FlushCurrentIfNeeded(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || len(s.current.entries) == 0 {
		return
	}
	if s.config.ChunkIdlePeriod > 0 && now.Sub(s.current.lastAppend) >= s.config.ChunkIdlePeriod {
		s.closeCurrentLocked()
		return
	}
	if s.config.MaxChunkAge > 0 && now.Sub(s.current.createdAt) >= s.config.MaxChunkAge {
		s.closeCurrentLocked()
	}
}

// TakePending returns and clears the list of closed chunks pending flush. Call from flush worker.
func (s *Stream) TakePending() []*chunkDesc {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pending
	s.pending = nil
	return out
}

// Labels returns a copy of the stream labels.
func (s *Stream) Labels() map[string]string {
	if s.labels == nil {
		return nil
	}
	out := make(map[string]string, len(s.labels))
	for k, v := range s.labels {
		out[k] = v
	}
	return out
}
