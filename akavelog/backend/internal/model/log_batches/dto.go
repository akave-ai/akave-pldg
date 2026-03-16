package logbatches

import (
	"time"

	"github.com/google/uuid"
)

// InsertParams carries the data needed to record one flushed chunk in the metadata index.
// Constructed by the ingester after a successful O3 PutObject.
type InsertParams struct {
	ProjectID   *uuid.UUID // nil → anonymous / default project
	Tenant      string     // e.g. "default"
	StreamID    string     // FNV-64a hex of sorted stream labels
	Service     string     // extracted from stream labels
	TsStart     time.Time
	TsEnd       time.Time
	Levels      []string          // distinct levels in the batch; may be empty
	Tags        map[string]string // raw stream labels stored as JSONB
	O3ObjectKey string            // full object key in Akave O3
	EntryCount  int
}

// QueryParams filters for log_batches lookups (used by the query engine in Phase 5).
type QueryParams struct {
	Tenant    string
	ProjectID *uuid.UUID
	Service   string
	Levels    []string
	TsStart   time.Time
	TsEnd     time.Time
}
