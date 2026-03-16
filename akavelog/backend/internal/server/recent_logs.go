package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/model"
)

const maxRecentLogs = 200

// RecentLogsStore keeps the last N ingested log entries for the demo UI.
type RecentLogsStore struct {
	mu      sync.RWMutex
	entries []recentLogEntry
}

type recentLogEntry struct {
	Entry    model.LogEntry `json:"entry"`
	Received time.Time      `json:"received_at"`
}

func newRecentLogsStore() *RecentLogsStore {
	return &RecentLogsStore{entries: make([]recentLogEntry, 0, maxRecentLogs)}
}

// Add parses raw as JSON log entry and appends; drops invalid.
func (s *RecentLogsStore) Add(raw []byte) {
	var e model.LogEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return
	}
	if e.Service == "" || e.Message == "" {
		return
	}
	s.AddEntry(&e)
}

// AddEntry appends a log entry (e.g. from push onLog callback).
func (s *RecentLogsStore) AddEntry(e *model.LogEntry) {
	if e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, recentLogEntry{Entry: *e, Received: time.Now().UTC()})
	if len(s.entries) > maxRecentLogs {
		s.entries = s.entries[len(s.entries)-maxRecentLogs:]
	}
}

// GetRecent returns a copy of recent entries (newest last).
func (s *RecentLogsStore) GetRecent() []recentLogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]recentLogEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// UploadStatusStore holds last flush info for the demo UI.
type UploadStatusStore struct {
	mu        sync.RWMutex
	LastAt    time.Time `json:"last_upload_at"`
	LastKey   string    `json:"last_upload_key"`
	LastCount int       `json:"last_upload_count"`
	Pending   int       `json:"pending_count"`
	BatcherOn bool      `json:"batcher_enabled"`
}

func (u *UploadStatusStore) SetLastFlush(count int, key string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.LastAt = time.Now().UTC()
	u.LastKey = key
	u.LastCount = count
	u.Pending = 0
}

func (u *UploadStatusStore) SetPending(n int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.Pending = n
}

func (u *UploadStatusStore) Get() UploadStatusStore {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return UploadStatusStore{
		LastAt:    u.LastAt,
		LastKey:   u.LastKey,
		LastCount: u.LastCount,
		Pending:   u.Pending,
		BatcherOn: u.BatcherOn,
	}
}
