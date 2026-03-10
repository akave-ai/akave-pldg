package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LogBatchRepository persists and retrieves log_batches metadata records.
// It is the write side of the metadata index (Phase 4) and the read side of the query
// engine (Phase 5). All methods are safe for concurrent use.
type LogBatchRepository struct {
	pool *pgxpool.Pool
}

// NewLogBatchRepository creates a repository backed by the given connection pool.
func NewLogBatchRepository(pool *pgxpool.Pool) *LogBatchRepository {
	return &LogBatchRepository{pool: pool}
}

// Insert writes one log_batches row after a successful O3 chunk upload.
// It is intentionally lightweight: no transaction needed because the O3 upload
// has already committed; if this insert fails the chunk is still safe in O3
// (the O3 Progress index is the fallback). Callers should log the error and continue.
func (r *LogBatchRepository) Insert(ctx context.Context, p logbatches.InsertParams) error {
	if p.Tenant == "" {
		p.Tenant = "default"
	}
	if p.TsStart.IsZero() || p.TsEnd.IsZero() {
		return fmt.Errorf("log_batch insert: ts_start and ts_end are required")
	}
	if p.O3ObjectKey == "" {
		return fmt.Errorf("log_batch insert: o3_object_key is required")
	}

	tagsJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return fmt.Errorf("log_batch insert: marshal tags: %w", err)
	}

	levels := p.Levels
	if levels == nil {
		levels = []string{}
	}

	const q = `
		INSERT INTO log_batches
			(project_id, tenant, stream_id, service, ts_start, ts_end, levels, tags, o3_object_key, entry_count)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = r.pool.Exec(ctx, q,
		p.ProjectID,   // $1  UUID | NULL
		p.Tenant,      // $2  TEXT
		p.StreamID,    // $3  TEXT
		p.Service,     // $4  TEXT
		p.TsStart,     // $5  TIMESTAMPTZ
		p.TsEnd,       // $6  TIMESTAMPTZ
		levels,        // $7  TEXT[]
		tagsJSON,      // $8  JSONB
		p.O3ObjectKey, // $9  TEXT
		p.EntryCount,  // $10 INT
	)
	if err != nil {
		return fmt.Errorf("log_batch insert: %w", err)
	}
	return nil
}

// ListByTimeRange returns all log_batches rows whose time range overlaps [from, to]
// for the given tenant. Used by the Phase 5 query engine to discover O3 object keys.
func (r *LogBatchRepository) ListByTimeRange(
	ctx context.Context,
	p logbatches.QueryParams,
) ([]logbatches.LogBatch, error) {
	if p.Tenant == "" {
		p.Tenant = "default"
	}
	if p.TsEnd.IsZero() {
		p.TsEnd = time.Now().UTC()
	}

	const q = `
		SELECT id, project_id, tenant, stream_id, service,
		       ts_start, ts_end, levels, tags, o3_object_key, entry_count, created_at
		FROM   log_batches
		WHERE  tenant   = $1
		  AND  ts_start <= $3
		  AND  ts_end   >= $2
		ORDER  BY ts_start ASC
	`
	rows, err := r.pool.Query(ctx, q, p.Tenant, p.TsStart, p.TsEnd)
	if err != nil {
		return nil, fmt.Errorf("log_batches list: %w", err)
	}
	defer rows.Close()

	var out []logbatches.LogBatch
	for rows.Next() {
		var b logbatches.LogBatch
		if err := rows.Scan(
			&b.ID, &b.ProjectID, &b.Tenant, &b.StreamID, &b.Service,
			&b.TsStart, &b.TsEnd, &b.Levels, &b.Tags, &b.O3ObjectKey,
			&b.EntryCount, &b.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("log_batches scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_batches rows: %w", err)
	}
	return out, nil
}
