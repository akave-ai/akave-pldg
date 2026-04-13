package integration

import (
	"context"
	"testing"

	"data-explorer/config"
	"data-explorer/database"
	"data-explorer/indexing"
	"data-explorer/utils"
)

// It will simulate complete indexer and after each chunk of blocks, it will check if there are any missing blocks in the database. If there are missing blocks, we try to
// debug and fix the issue. This test will help us to identify any issues in the block indexing logic and ensure that all blocks are indexed correctly.
func TestMissingBlocks(t *testing.T) {
	ctx := context.Background()
	cfg, _ := config.DefaultBackfillConfig()
	cfg.FromBlock = 0
	cfg.ToBlock = 1000

	db, err := database.NewDB(database.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("failed to close database: %v", err)
		}
	}()

	handler := indexing.DBHandler(db, "default", cfg)

	for i := 0; i < 1000; i += 100 {
		cfg.FromBlock = uint64(i)
		cfg.ToBlock = uint64(i + 100)
		err = indexing.Backfill(ctx, cfg, nil, handler)
		if err != nil {
			t.Fatalf("failed to backfill: %v", err)
		}

		stats, err := db.GetLastIndexedBlock(ctx, "default")
		if err != nil {
			t.Fatalf("failed to get last indexed block: %v", err)
		}

		if stats != int64(i+100) {
			t.Errorf("expected last indexed block %d, got %d", i+100, stats)
		} else {
			t.Logf("successfully indexed blocks up to %d", stats)
		}

		// Check for missing blocks in the database
		missingBlocks, err := db.GetMissingBlocks(ctx, "default", int64(i), int64(i+100))
		if err != nil {
			t.Fatalf("failed to get missing blocks: %v", err)
		}
		if len(missingBlocks) > 0 {
			t.Errorf("missing blocks between %d and %d: %v", i, i+100, missingBlocks)
		}
		if len(missingBlocks) == 0 {
			t.Logf("no missing blocks between %d and %d", i, i+100)
			t.Logf("... all blocks indexed successfully ...")
			t.Logf("... checking next chunk of blocks ...")
			t.Logf("... skipping middle events ...")
		} else {
			// try to fetch each missing block and see if we can identify any issues
			for _, blockNum := range missingBlocks {
				block, err := utils.NewRpcUrl(cfg.RPCURL).GetBlockByNumber(ctx, 1, cfg.MaxRetry, uint64(blockNum))
				if err != nil {
					t.Logf("failed to fetch missing block %d: %v", blockNum, err)
				} else {
					t.Logf("successfully fetched missing block %d: hash %x", blockNum, block.Hash)
				}
			}
		}
	}
}