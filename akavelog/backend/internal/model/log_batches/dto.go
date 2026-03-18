package logbatches

import (
	"time"

	"github.com/google/uuid"
)

// InsertParams carries the data needed to record one flushed chunk in the metadata index.
type InsertParams struct {
	ProjectID   *uuid.UUID
	Tenant      string
	StreamID    string
	Service     string
	TsStart     time.Time
	TsEnd       time.Time
	Levels      []string
	Tags        map[string]string
	O3ObjectKey string
	EntryCount  int
}

// QueryParams filters for log_batches lookups.
type QueryParams struct {
	Tenant    string
	ProjectID *uuid.UUID
	Service   string   // optional; empty = all services
	Levels    []string // optional; empty = all levels
	TsStart   time.Time
	TsEnd     time.Time
}
