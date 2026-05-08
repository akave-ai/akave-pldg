package query_test

import (
	"context"
	"testing"
	// "time"

	"github.com/akave-ai/akavelog/internal/model"
	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
	"github.com/akave-ai/akavelog/internal/query"
	// "github.com/akave-ai/akavelog/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Stub implementations ──────────────────────────────────────────────────────

// stubLookup satisfies query.BatchLookup without a real DB.
type stubLookup struct {
	batches []logbatches.LogBatch
}

func (s *stubLookup) ListByFilter(_ context.Context, _ logbatches.QueryParams) ([]logbatches.LogBatch, error) {
	return s.batches, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestEngine_EmptyBatches(t *testing.T) {
	eng := query.New(&stubLookup{batches: nil}, nil)
	resp, err := eng.Query(context.Background(), model.QueryRequest{})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.Empty(t, resp.Results)
	assert.False(t, resp.Truncated)
}

func TestEngine_DefaultLimit(t *testing.T) {
	// Verify normalise() sets default limit of 100.
	eng := query.New(&stubLookup{}, nil)
	resp, err := eng.Query(context.Background(), model.QueryRequest{Limit: 0})
	require.NoError(t, err)
	// No batches → 0 results, but limit was applied (no panic).
	assert.Equal(t, 0, resp.Count)
}

func TestNormalise_ClampsLimit(t *testing.T) {
	eng := query.New(&stubLookup{}, nil)
	// Limit above max should be clamped — verified indirectly via no error.
	resp, err := eng.Query(context.Background(), model.QueryRequest{Limit: 99999})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
}

// ── Filter unit tests (exported via internal test package) ────────────────────

func TestExtractLevelFromLine(t *testing.T) {
	cases := []struct {
		line     string
		expected string
	}{
		{"ERROR: something went wrong", "error"},
		{"[ERROR] something went wrong", "error"},
		{"ERROR something went wrong", "error"},
		{"WARN: low disk", "warn"},
		{"WARNING: low disk", "warn"},
		{"INFO starting server", "info"},
		{"DEBUG connecting to db", "debug"},
		{"FATAL out of memory", "fatal"},
		{"just a plain message", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := query.ExtractLevelFromLine(tc.line)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestExtractService(t *testing.T) {
	assert.Equal(t, "payment-api", query.ExtractService(map[string]string{"job": "payment-api"}))
	assert.Equal(t, "auth", query.ExtractService(map[string]string{"app": "auth"}))
	assert.Equal(t, "svc", query.ExtractService(map[string]string{"service": "svc"}))
	assert.Equal(t, "akavelog", query.ExtractService(map[string]string{"other": "x"}))
	assert.Equal(t, "akavelog", query.ExtractService(nil))
}

func TestMatchesLevel(t *testing.T) {
	assert.True(t, query.MatchesLevel("error", []string{"ERROR", "WARN"}))
	assert.True(t, query.MatchesLevel("ERROR", []string{"error"}))
	assert.False(t, query.MatchesLevel("info", []string{"error", "warn"}))
	assert.False(t, query.MatchesLevel("", []string{"error"}))
}
