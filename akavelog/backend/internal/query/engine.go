package query

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/akave-ai/akavelog/internal/model"
	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
	"github.com/akave-ai/akavelog/internal/storage"
)

const (
	defaultLimit    = 100
	maxLimit        = 1000
	defaultLookback = 30 * 24 * time.Hour // 30 days
)

// BatchLookup is the interface the engine uses to find candidate O3 object keys.
// Implemented by *repository.LogBatchRepository.
type BatchLookup interface {
	ListByFilter(ctx context.Context, p logbatches.QueryParams) ([]logbatches.LogBatch, error)
}

// Engine executes log queries: metadata lookup → O3 fetch → decompress → filter.
// All methods are safe for concurrent use.
type Engine struct {
	lookup   BatchLookup
	o3Client *storage.O3Client
}

// New creates a query engine.
func New(lookup BatchLookup, o3Client *storage.O3Client) *Engine {
	return &Engine{lookup: lookup, o3Client: o3Client}
}

// Query executes a full query and returns matching log entries.
//
// Steps:
//  1. Normalise request (fill defaults, clamp limit)
//  2. Query log_batches via metadata lookup (SQL)
//  3. For each candidate chunk: GetObject → gunzip → unmarshal
//  4. Apply per-entry filters (time, level, keyword)
//  5. Return up to limit results
func (e *Engine) Query(ctx context.Context, req model.QueryRequest) (*model.QueryResponse, error) {
	req = normalise(req)

	// ── 1. Metadata lookup ────────────────────────────────────────────────────
	batches, err := e.lookup.ListByFilter(ctx, logbatches.QueryParams{
		Tenant:  req.Tenant,
		Service: req.Service,
		Levels:  req.Levels,
		TsStart: req.TsStart,
		TsEnd:   req.TsEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("query metadata lookup: %w", err)
	}

	if len(batches) == 0 {
		return &model.QueryResponse{Results: []model.QueryResultEntry{}, Count: 0}, nil
	}

	// ── 2. Fetch, decompress, filter ──────────────────────────────────────────
	var results []model.QueryResultEntry
	truncated := false

	for _, batch := range batches {
		if len(results) >= req.Limit {
			truncated = true
			break
		}

		entries, err := e.fetchAndFilter(ctx, batch, req)
		if err != nil {
			// Non-fatal: one bad/missing chunk should not fail the whole query.
			log.Printf("[query] fetch/filter %s: %v", batch.O3ObjectKey, err)
			continue
		}

		for _, entry := range entries {
			if len(results) >= req.Limit {
				truncated = true
				break
			}
			results = append(results, entry)
		}
	}

	if results == nil {
		results = []model.QueryResultEntry{}
	}

	return &model.QueryResponse{
		Results:   results,
		Count:     len(results),
		Truncated: truncated,
	}, nil
}

// fetchAndFilter downloads one O3 chunk, decompresses it, and returns entries
// that pass all per-entry filters.
func (e *Engine) fetchAndFilter(
	ctx context.Context,
	batch logbatches.LogBatch,
	req model.QueryRequest,
) ([]model.QueryResultEntry, error) {
	// ── GetObject ─────────────────────────────────────────────────────────────
	raw, err := e.o3Client.GetObject(ctx, batch.O3ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}

	// ── Gunzip ────────────────────────────────────────────────────────────────
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer zr.Close()

	decompressed, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}

	// ── Unmarshal chunk ───────────────────────────────────────────────────────
	// Chunk format written by internal/chunk/o3.go:
	// {"labels":{...},"entries":[{"ts_ns":...,"line":"..."},...]}
	var payload chunkPayload
	if err := json.Unmarshal(decompressed, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal chunk: %w", err)
	}

	service := ExtractService(payload.Labels)

	// ── Per-entry filter ──────────────────────────────────────────────────────
	var out []model.QueryResultEntry
	for _, ce := range payload.Entries {
		if !matchesEntry(ce, payload.Labels, req) {
			continue
		}
		level := extractLevel(payload.Labels)
		if l := ExtractLevelFromLine(ce.Line); l != "" {
			level = l
		}
		out = append(out, model.QueryResultEntry{
			TsNs:        ce.TsNs,
			Timestamp:   time.Unix(0, ce.TsNs).UTC().Format(time.RFC3339Nano),
			Service:     service,
			Level:       level,
			Line:        ce.Line,
			Labels:      payload.Labels,
			O3ObjectKey: batch.O3ObjectKey,
		})
	}
	// Reverse so entries within the chunk are newest-first,
	// consistent with the DESC batch ordering from the SQL layer.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ── Chunk payload (mirrors internal/chunk/o3.go — kept local to avoid coupling) ──

type chunkPayload struct {
	Labels  map[string]string `json:"labels"`
	Entries []chunkEntry      `json:"entries"`
}

type chunkEntry struct {
	TsNs int64  `json:"ts_ns"`
	Line string `json:"line"`
}

// ── Filter helpers ─────────────────────────────────────────────────────────────

// matchesEntry returns true if the entry passes all active filters.
func matchesEntry(ce chunkEntry, labels map[string]string, req model.QueryRequest) bool {
	// Time bounds at entry level (chunk-level pre-filter already done in SQL).
	if req.TsStart.UnixNano() > 0 && ce.TsNs < req.TsStart.UnixNano() {
		return false
	}
	if ce.TsNs > req.TsEnd.Add(time.Second).UnixNano() {
		return false
	}

	// Level filter.
	if len(req.Levels) > 0 {
		entryLevel := extractLevel(labels)
		if l := ExtractLevelFromLine(ce.Line); l != "" {
			entryLevel = l
		}
		if !MatchesLevel(entryLevel, req.Levels) {
			return false
		}
	}

	// Keyword filter: case-insensitive substring.
	if req.Keyword != "" {
		if !strings.Contains(strings.ToLower(ce.Line), strings.ToLower(req.Keyword)) {
			return false
		}
	}

	return true
}

// matchesLevel returns true if entryLevel is in the allowed set (case-insensitive).
func MatchesLevel(entryLevel string, allowed []string) bool {
	el := strings.ToUpper(entryLevel)
	for _, a := range allowed {
		if strings.ToUpper(a) == el {
			return true
		}
	}
	return false
}

// extractService mirrors ingester.buildInsertParams: job > app > service > fallback.
func ExtractService(labels map[string]string) string {
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

// extractLevel returns the "level" stream label if present.
func extractLevel(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return labels["level"]
}

// extractLevelFromLine detects a log level at the start of a log line.
// Handles: "ERROR: msg", "[ERROR] msg", "ERROR msg".
func ExtractLevelFromLine(line string) string {
	upper := strings.ToUpper(line)
	for _, lvl := range []string{"FATAL", "ERROR", "WARN", "WARNING", "INFO", "DEBUG", "TRACE"} {
		if strings.HasPrefix(upper, lvl+":") ||
			strings.HasPrefix(upper, "["+lvl+"]") ||
			strings.HasPrefix(upper, lvl+" ") {
			if lvl == "WARNING" {
				return "warn"
			}
			return strings.ToLower(lvl)
		}
	}
	return ""
}

// normalise fills in defaults and clamps the limit.
func normalise(req model.QueryRequest) model.QueryRequest {
	if req.Tenant == "" {
		req.Tenant = "default"
	}
	if req.TsEnd.IsZero() {
		req.TsEnd = time.Now().UTC()
	}
	if req.TsStart.IsZero() {
		req.TsStart = req.TsEnd.Add(-defaultLookback)
	}
	if req.Limit <= 0 {
		req.Limit = defaultLimit
	}
	if req.Limit > maxLimit {
		req.Limit = maxLimit
	}
	return req
}

// QueryStream executes a query and calls onEntry for each matching log entry
// as soon as it is found, without buffering all results first.
// onEntry returning an error (e.g. client disconnect) aborts the stream early.
func (e *Engine) QueryStream(
	ctx context.Context,
	req model.QueryRequest,
	onEntry func(entry model.QueryResultEntry) error,
) (count int, truncated bool, err error) {
	req = normalise(req)

	batches, err := e.lookup.ListByFilter(ctx, logbatches.QueryParams{
		Tenant:  req.Tenant,
		Service: req.Service,
		Levels:  req.Levels,
		TsStart: req.TsStart,
		TsEnd:   req.TsEnd,
	})
	if err != nil {
		return 0, false, fmt.Errorf("query metadata lookup: %w", err)
	}

	for _, batch := range batches {
		if count >= req.Limit {
			truncated = true
			break
		}

		entries, err := e.fetchAndFilter(ctx, batch, req)
		if err != nil {
			log.Printf("[query] stream fetch/filter %s: %v", batch.O3ObjectKey, err)
			continue
		}

		for _, entry := range entries {
			if count >= req.Limit {
				truncated = true
				break
			}
			if err := onEntry(entry); err != nil {
				// Client disconnected or handler signalled stop.
				return count, false, nil
			}
			count++
		}
	}

	return count, truncated, nil
}
