package logbatches

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// LogBatch is the metadata record written to PostgreSQL after a chunk is flushed to Akave O3.
// It enables fast query-time discovery of O3 objects without scanning object storage directly.
type LogBatch struct {
	ID          uuid.UUID       `db:"id"`
	ProjectID   *uuid.UUID      `db:"project_id"` // nil when tenant is anonymous / default
	Tenant      string          `db:"tenant"`
	StreamID    string          `db:"stream_id"` // FNV-64a hex hash of sorted stream labels
	Service     string          `db:"service"`   // extracted from stream labels (job / app / service)
	TsStart     time.Time       `db:"ts_start"`
	TsEnd       time.Time       `db:"ts_end"`
	Levels      []string        `db:"levels"` // distinct log levels present in the batch
	Tags        json.RawMessage `db:"tags"`   // JSONB – raw stream labels
	O3ObjectKey string          `db:"o3_object_key"`
	EntryCount  int             `db:"entry_count"`
	CreatedAt   time.Time       `db:"created_at"`
}
