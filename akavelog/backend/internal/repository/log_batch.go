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
// Write side: Phase 4 (ingester calls Insert after each O3 upload).
// Read side:  Phase 5 (query engine calls ListByFilter to discover O3 keys).
type LogBatchRepository struct {
	pool *pgxpool.Pool
}

// NewLogBatchRepository creates a repository backed by the given connection pool.
func NewLogBatchRepository(pool *pgxpool.Pool) *LogBatchRepository {
	return &LogBatchRepository{pool: pool}
}

// Insert writes one log_batches row after a successful O3 chunk upload.
// Non-transactional by design: if this fails the chunk is still safe in O3.
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
		p.ProjectID,
		p.Tenant,
		p.StreamID,
		p.Service,
		p.TsStart,
		p.TsEnd,
		levels,
		tagsJSON,
		p.O3ObjectKey,
		p.EntryCount,
	)
	if err != nil {
		return fmt.Errorf("log_batch insert: %w", err)
	}
	return nil
}

// ListByTimeRange returns all log_batches rows whose time range overlaps [from, to].
// Used by GET /index/batches (debug/ops endpoint).
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

// ListByFilter returns log_batches rows matching the given filters.
// Used by the Phase 5 query engine. Service and Levels are optional.
func (r *LogBatchRepository) ListByFilter(
	ctx context.Context,
	p logbatches.QueryParams,
) ([]logbatches.LogBatch, error) {
	if p.Tenant == "" {
		p.Tenant = "default"
	}
	if p.TsEnd.IsZero() {
		p.TsEnd = time.Now().UTC()
	}

	args := []any{p.Tenant, p.TsStart, p.TsEnd}
	where := `
		WHERE  tenant   = $1
		  AND  ts_start <= $3
		  AND  ts_end   >= $2`

	if p.Service != "" {
		args = append(args, p.Service)
		where += fmt.Sprintf("\n\t\t  AND  service  = $%d", len(args))
	}

	if len(p.Levels) > 0 {
		args = append(args, p.Levels)
		where += fmt.Sprintf("\n\t\t  AND  levels   && $%d::text[]", len(args))
	}

	q := `
		SELECT id, project_id, tenant, stream_id, service,
		       ts_start, ts_end, levels, tags, o3_object_key, entry_count, created_at
		FROM   log_batches` + where + `
		ORDER  BY ts_start ASC`

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("log_batches filter: %w", err)
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
			return nil, fmt.Errorf("log_batches filter scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_batches filter rows: %w", err)
	}
	return out, nil
}
