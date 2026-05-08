package ingester

import (
	"sync"

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/push"
)

// Instance holds streams for one tenant.
type Instance struct {
	tenant  string
	config  StreamConfig
	mu      sync.RWMutex
	streams map[string]*Stream // key = chunk.StreamID(labels)
	onChunk func(tenant string, labels map[string]string, desc *chunkDesc)
}

// newInstance creates an instance. onChunk is called when a chunk is closed (tenant, labels, desc).
func newInstance(tenant string, config StreamConfig, onChunk func(tenant string, labels map[string]string, desc *chunkDesc)) *Instance {
	return &Instance{
		tenant:  tenant,
		config:  config,
		streams: make(map[string]*Stream),
		onChunk: onChunk,
	}
}

func copyLabels(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Push appends the request's streams/entries to the instance.
func (i *Instance) Push(req *push.PushRequest) {
	for _, sref := range req.Streams {
		if len(sref.Entries) == 0 {
			continue
		}
		streamID := chunk.StreamID(sref.Labels)
		labelsCopy := copyLabels(sref.Labels)
		i.mu.RLock()
		s := i.streams[streamID]
		i.mu.RUnlock()
		if s == nil {
			i.mu.Lock()
			s = i.streams[streamID]
			if s == nil {
				s = newStream(labelsCopy, i.config, func(desc *chunkDesc) {
					if i.onChunk != nil {
						i.onChunk(i.tenant, copyLabels(labelsCopy), desc)
					}
				})
				i.streams[streamID] = s
			}
			i.mu.Unlock()
		}
		s.Push(sref.Entries)
	}
}

// ForEachStream calls fn for each stream (for sweep). fn must not block.
func (i *Instance) ForEachStream(fn func(*Stream)) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, s := range i.streams {
		fn(s)
	}
}
