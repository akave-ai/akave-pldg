package indexing

import (
	"context"
	"testing"

	"data-explorer/config"
	"data-explorer/database"
	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
)

func TestNoOpHandler(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		ev   *utils.DecodedEvent
	}{
		{"nil event", nil},
		{"empty event", &utils.DecodedEvent{}},
		{"CreateBucket event", &utils.DecodedEvent{
			EventName:       "CreateBucket",
			ContractAddress: common.HexToAddress("0xbFAbD47bF901ca1341D128DDD06463AA476E970B"),
			BlockNumber:     1000,
			TxHash:          common.Hash{1},
			LogIndex:        0,
			Data:            map[string]interface{}{"id": "0x01"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := NoOpHandler(ctx, tt.ev); err != nil {
				t.Errorf("NoOpHandler() error = %v, want nil", err)
			}
		})
	}
}

func TestLoggingHandler(t *testing.T) {
	ctx := context.Background()

	ev := &utils.DecodedEvent{
		EventName:       "CreateFile",
		ContractAddress: common.HexToAddress("0xbFAbD47bF901ca1341D128DDD06463AA476E970B"),
		BlockNumber:     2000,
		TxHash:          common.Hash{2},
		LogIndex:        1,
		Data:            map[string]interface{}{"bucket_id": "0xab"},
	}

	if err := LoggingHandler(ctx, ev); err != nil {
		t.Errorf("LoggingHandler() error = %v, want nil", err)
	}
}

func TestBackfill_InvalidRange(t *testing.T) {
	ctx := context.Background()
	cfg, _ := config.DefaultBackfillConfig()
	cfg.FromBlock = 1000
	cfg.ToBlock = 500 // from > to

	err := Backfill(ctx, cfg, NoOpHandler, nil)
	if err == nil {
		t.Fatal("Backfill() expected error for invalid range, got nil")
	}
	if want := "fromBlock 1000 > toBlock 500"; err.Error() != want {
		t.Errorf("Backfill() error = %q, want %q", err.Error(), want)
	}
}

func TestBackfill_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg, _ := config.DefaultBackfillConfig()
	cfg.FromBlock = 0
	cfg.ToBlock = 0 // will try GetBlockNumber first

	err := Backfill(ctx, cfg, NoOpHandler, nil)
	if err == nil {
		t.Fatal("Backfill() expected error with cancelled context, got nil")
	}
	// Should get context.Canceled or error from failed RPC
	if ctx.Err() == nil {
		t.Error("context should be cancelled")
	}
}

func TestBackfill_EmptyRange(t *testing.T) {
	// Use a block range that requires GetLogs; RPC will fail without network,
	// but we verify Backfill is invoked and returns an error (not panic).
	ctx := context.Background()
	cfg, _ := config.DefaultBackfillConfig()
	cfg.RPCURL = "http://127.0.0.1:" // unreachable
	cfg.FromBlock = 1
	cfg.ToBlock = 1

	err := Backfill(ctx, cfg, NoOpHandler, nil)
	if err == nil {
		t.Fatal("Backfill() expected error (no RPC), got nil")
	}
}

func TestBackfill_BatchHandler(t *testing.T) {
	ctx := context.Background()
	cfg, _ := config.DefaultBackfillConfig()
	cfg.FromBlock = 0
	cfg.ToBlock = 0

	db, err := database.NewDB(database.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("failed to close database: %v", err)
		}
	}()

	batchHandler := DBHandler(db, "default", cfg)

	totalBlocks := 100
	cfg.FromBlock = 0
	cfg.ToBlock = uint64(totalBlocks)

	for i := 0; i < totalBlocks; i += 10 {
		cfg.FromBlock = uint64(i)
		cfg.ToBlock = uint64(i + 10)
		err = Backfill(ctx, cfg, nil, batchHandler)
		if err != nil {
			t.Fatalf("failed to backfill: %v", err)
		}
	}

	stats, err := db.GetLastIndexedBlock(ctx, "default")
	if err != nil {
		t.Fatalf("failed to get last indexed block: %v", err)
	}

	if stats != int64(totalBlocks) {
		t.Errorf("expected last indexed block %d, got %d", totalBlocks, stats)
	}

	//check events data in db
	events, err := db.GetEventsByBlockRange(ctx, 0, int64(totalBlocks))
	if err != nil {
		t.Fatalf("failed to get events by block range: %v", err)
	}

	if len(events) == 0 {
		t.Errorf("expected some events in the database, got 0")
	}
}
