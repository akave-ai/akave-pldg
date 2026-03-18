package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPool returns a pgxpool connected to the test database.
// Set TEST_DATABASE_URL to override; defaults to the local docker-compose DSN.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://akavelog:akavelog@localhost:5433/akavelog?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "connect to test DB")
	t.Cleanup(pool.Close)
	return pool
}

// cleanupBatches removes rows inserted during a test so tests stay idempotent.
func cleanupBatches(t *testing.T, pool *pgxpool.Pool, streamID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"DELETE FROM log_batches WHERE stream_id = $1", streamID)
	require.NoError(t, err)
}

func TestLogBatchRepository_Insert(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-stream-insert-001"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	now := time.Now().UTC().Truncate(time.Second)
	params := logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    streamID,
		Service:     "payment-api",
		TsStart:     now.Add(-5 * time.Second),
		TsEnd:       now,
		Levels:      []string{"info", "error"},
		Tags:        map[string]string{"job": "payment-api", "env": "test"},
		O3ObjectKey: "chunks/default/" + streamID + "/1000_2000.json.gz",
		EntryCount:  42,
	}

	err := repo.Insert(context.Background(), params)
	require.NoError(t, err)
}

func TestLogBatchRepository_Insert_RequiresTsStart(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	err := repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    "test-stream-invalid",
		O3ObjectKey: "chunks/default/x/1_2.json.gz",
		// TsStart / TsEnd intentionally zero
	})
	require.Error(t, err, "should reject zero timestamps")
}

func TestLogBatchRepository_Insert_RequiresO3Key(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	now := time.Now().UTC()
	err := repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:   "default",
		StreamID: "test-stream-invalid",
		TsStart:  now.Add(-time.Second),
		TsEnd:    now,
		// O3ObjectKey intentionally empty
	})
	require.Error(t, err, "should reject empty o3_object_key")
}

func TestLogBatchRepository_ListByTimeRange(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-stream-list-001"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	base := time.Now().UTC().Truncate(time.Second)

	// Insert three batches at different times.
	batches := []logbatches.InsertParams{
		{
			Tenant:      "default",
			StreamID:    streamID,
			Service:     "auth-api",
			TsStart:     base.Add(-30 * time.Minute),
			TsEnd:       base.Add(-25 * time.Minute),
			Levels:      []string{"info"},
			Tags:        map[string]string{"job": "auth-api"},
			O3ObjectKey: "chunks/default/" + streamID + "/1_2.json.gz",
			EntryCount:  10,
		},
		{
			Tenant:      "default",
			StreamID:    streamID,
			Service:     "auth-api",
			TsStart:     base.Add(-20 * time.Minute),
			TsEnd:       base.Add(-15 * time.Minute),
			Levels:      []string{"warn"},
			Tags:        map[string]string{"job": "auth-api"},
			O3ObjectKey: "chunks/default/" + streamID + "/3_4.json.gz",
			EntryCount:  20,
		},
		{
			Tenant:      "default",
			StreamID:    streamID,
			Service:     "auth-api",
			TsStart:     base.Add(-5 * time.Minute),
			TsEnd:       base,
			Levels:      []string{"error"},
			Tags:        map[string]string{"job": "auth-api"},
			O3ObjectKey: "chunks/default/" + streamID + "/5_6.json.gz",
			EntryCount:  5,
		},
	}
	for _, p := range batches {
		require.NoError(t, repo.Insert(context.Background(), p))
	}

	t.Run("returns all overlapping batches", func(t *testing.T) {
		results, err := repo.ListByTimeRange(context.Background(), logbatches.QueryParams{
			Tenant:  "default",
			TsStart: base.Add(-35 * time.Minute),
			TsEnd:   base,
		})
		require.NoError(t, err)
		// All three batches must be present (by stream ID).
		keys := make(map[string]bool)
		for _, b := range results {
			keys[b.O3ObjectKey] = true
		}
		assert.True(t, keys["chunks/default/"+streamID+"/1_2.json.gz"])
		assert.True(t, keys["chunks/default/"+streamID+"/3_4.json.gz"])
		assert.True(t, keys["chunks/default/"+streamID+"/5_6.json.gz"])
	})

	t.Run("filters by time range – only recent", func(t *testing.T) {
		results, err := repo.ListByTimeRange(context.Background(), logbatches.QueryParams{
			Tenant:  "default",
			TsStart: base.Add(-10 * time.Minute),
			TsEnd:   base,
		})
		require.NoError(t, err)
		keys := make(map[string]bool)
		for _, b := range results {
			keys[b.O3ObjectKey] = true
		}
		assert.True(t, keys["chunks/default/"+streamID+"/5_6.json.gz"],
			"recent batch should be included")
		assert.False(t, keys["chunks/default/"+streamID+"/1_2.json.gz"],
			"old batch should be excluded")
	})

	t.Run("returns empty slice for future range", func(t *testing.T) {
		results, err := repo.ListByTimeRange(context.Background(), logbatches.QueryParams{
			Tenant:  "default",
			TsStart: base.Add(1 * time.Hour),
			TsEnd:   base.Add(2 * time.Hour),
		})
		require.NoError(t, err)
		// May return other rows from other tests but none from our stream.
		for _, b := range results {
			assert.NotEqual(t, streamID, b.StreamID)
		}
	})
}

func TestLogBatchRepository_NilLevels(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-stream-nil-levels"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	now := time.Now().UTC()
	err := repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    streamID,
		Service:     "svc",
		TsStart:     now.Add(-time.Second),
		TsEnd:       now,
		Levels:      nil, // should be coerced to empty array
		Tags:        nil,
		O3ObjectKey: "chunks/default/" + streamID + "/1_2.json.gz",
		EntryCount:  1,
	})
	require.NoError(t, err, "nil levels should be coerced to empty array")
}

func TestLogBatchRepository_ListByFilter_NoFilters(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-filter-nofilter-001"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    streamID,
		Service:     "svc-a",
		TsStart:     now.Add(-5 * time.Minute),
		TsEnd:       now,
		Levels:      []string{"info"},
		Tags:        map[string]string{"job": "svc-a"},
		O3ObjectKey: "chunks/default/" + streamID + "/1_2.json.gz",
		EntryCount:  3,
	}))

	results, err := repo.ListByFilter(context.Background(), logbatches.QueryParams{
		Tenant:  "default",
		TsStart: now.Add(-10 * time.Minute),
		TsEnd:   now.Add(time.Minute),
	})
	require.NoError(t, err)

	found := false
	for _, b := range results {
		if b.StreamID == streamID {
			found = true
		}
	}
	assert.True(t, found, "inserted batch should appear in results")
}

func TestLogBatchRepository_ListByFilter_ServiceFilter(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-filter-service-001"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	now := time.Now().UTC().Truncate(time.Second)

	// Insert two batches with different services.
	for _, svc := range []string{"auth-api", "payment-api"} {
		require.NoError(t, repo.Insert(context.Background(), logbatches.InsertParams{
			Tenant:      "default",
			StreamID:    streamID,
			Service:     svc,
			TsStart:     now.Add(-5 * time.Minute),
			TsEnd:       now,
			Levels:      []string{"info"},
			Tags:        map[string]string{"job": svc},
			O3ObjectKey: "chunks/default/" + streamID + "/" + svc + ".json.gz",
			EntryCount:  1,
		}))
	}

	results, err := repo.ListByFilter(context.Background(), logbatches.QueryParams{
		Tenant:  "default",
		Service: "auth-api",
		TsStart: now.Add(-10 * time.Minute),
		TsEnd:   now.Add(time.Minute),
	})
	require.NoError(t, err)

	for _, b := range results {
		if b.StreamID == streamID {
			assert.Equal(t, "auth-api", b.Service, "only auth-api should be returned")
		}
	}
}

func TestLogBatchRepository_ListByFilter_LevelsFilter(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewLogBatchRepository(pool)

	streamID := "test-filter-levels-001"
	t.Cleanup(func() { cleanupBatches(t, pool, streamID) })

	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    streamID,
		Service:     "svc",
		TsStart:     now.Add(-5 * time.Minute),
		TsEnd:       now,
		Levels:      []string{"error"},
		Tags:        map[string]string{"job": "svc"},
		O3ObjectKey: "chunks/default/" + streamID + "/error.json.gz",
		EntryCount:  1,
	}))
	require.NoError(t, repo.Insert(context.Background(), logbatches.InsertParams{
		Tenant:      "default",
		StreamID:    streamID,
		Service:     "svc",
		TsStart:     now.Add(-4 * time.Minute),
		TsEnd:       now,
		Levels:      []string{"info"},
		Tags:        map[string]string{"job": "svc"},
		O3ObjectKey: "chunks/default/" + streamID + "/info.json.gz",
		EntryCount:  1,
	}))

	results, err := repo.ListByFilter(context.Background(), logbatches.QueryParams{
		Tenant:  "default",
		Levels:  []string{"error"},
		TsStart: now.Add(-10 * time.Minute),
		TsEnd:   now.Add(time.Minute),
	})
	require.NoError(t, err)

	for _, b := range results {
		if b.StreamID == streamID {
			assert.Contains(t, b.O3ObjectKey, "error",
				"only error-level batch should be returned")
		}
	}
}
