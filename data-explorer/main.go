package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"data-explorer/api"
	"data-explorer/config"
	"data-explorer/database"
	"data-explorer/indexing"
)

func main() {
	// ── Database ─────────────────────────────────────────────────────────────
	dbCfg := database.DefaultConfig()
	if v := os.Getenv("DB_HOST"); v != "" {
		dbCfg.Host = v
	}
	if v := os.Getenv("DB_USER"); v != "" {
		dbCfg.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		dbCfg.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		dbCfg.DBName = v
	}

	db, err := database.NewDB(dbCfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	log.Println("database connected")

	idxCtx, idxCancel := context.WithCancel(context.Background())
	defer idxCancel()

	bfCfg, err := config.DefaultBackfillConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if v := os.Getenv("RPC_URL"); v != "" {
		bfCfg.RPCURL = v
	}
	if v := os.Getenv("CHUNK_SIZE"); v != "" {
		if n, convErr := strconv.ParseUint(v, 10, 64); convErr == nil && n > 0 {
			bfCfg.ChunkSize = n
		}
	}

	// Resume from the last indexed block persisted in the DB.
	if lastIndexed, dbErr := db.GetLastIndexedBlock(context.Background(), bfCfg.ChainID); dbErr == nil && lastIndexed > 0 {
		log.Printf("indexer: resuming from last indexed block %d", lastIndexed)
		bfCfg.FromBlock = uint64(lastIndexed + 1)
	} else {
		if v := os.Getenv("BACKFILL_FROM"); v != "" {
			if n, convErr := strconv.ParseUint(v, 10, 64); convErr == nil {
				bfCfg.FromBlock = n
			}
		}
	}

	handler := indexing.DBHandler(db, bfCfg.ChainID, bfCfg)

	// ── Indexer: backfill then live ───────────────────────────────────────────
	go func() {
		// Phase 1 — historical backfill (catches up to the latest block).
		log.Println("indexer: starting backfill")
		if err := indexing.Backfill(idxCtx, bfCfg, indexing.NoOpHandler, handler); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("indexer: backfill stopped with error: %v", err)
			// Continue to live indexing anyway so new blocks are not missed.
		}
		log.Println("indexer: backfill complete, switching to live indexing")

		lastIndexed, dbErr := db.GetLastIndexedBlock(idxCtx, bfCfg.ChainID)
		if dbErr != nil || lastIndexed <= 0 {
			lastIndexed = int64(bfCfg.ToBlock)
		}

		if err := indexing.LiveIndexing(idxCtx, bfCfg.RPCURL, indexing.NoOpHandler, handler, int(lastIndexed), int(bfCfg.ChunkSize)); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("indexer: live indexing stopped: %v", err)
			}
		}
	}()

	// ── Missed-tx retry: runs every 30 minutes regardless of indexing phase ──
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("indexer: retrying failed transactions")
				if err := indexing.RetryFailedTxs(context.Background(), db, bfCfg.RPCURL, bfCfg.MaxRetry); err != nil {
					log.Printf("indexer: RetryFailedTxs error: %v", err)
				}
			case <-idxCtx.Done():
				return
			}
		}
	}()

	// ── API Server ───────────────────────────────────────────────────────────
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := api.NewServer(db, addr)

	go func() {
		log.Printf("api: listening on %s", addr)
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("api: %v", err)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down…")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()

	idxCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("api shutdown: %v", err)
	}
	log.Println("done")
}
