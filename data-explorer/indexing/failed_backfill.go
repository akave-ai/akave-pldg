package indexing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"data-explorer/database"
	"data-explorer/decoding"
	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
)

const (
	defaultMaxRetries = 5
	defaultBatchSize  = 500
)

// RetryFailedTxs fetches all pending failed transactions from the database,
// attempts to re-decode each one via the RPC node, and updates their status:
//   - decoded successfully  → inserted into actions, status set to 'resolved'
//   - permanent error       → status set to 'abandoned'
//   - transient error       → retry_count incremented, stays 'pending'
//
// Call this as a one-off job or on a cron-style loop after regular backfill.
func RetryFailedTxs(ctx context.Context, db *database.DB, rpcURL string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	rpc := utils.NewRpcUrl(rpcURL)

	pending, err := db.ListPendingFailedTxs(ctx, maxRetries, defaultBatchSize)
	if err != nil {
		return fmt.Errorf("list pending failed txs: %w", err)
	}
	if len(pending) == 0 {
		slog.Info("no pending failed txs to retry")
		return nil
	}
	slog.Info("retrying failed txs", "count", len(pending))

	resolved, abandoned, transient := 0, 0, 0

	for _, rec := range pending {
		txHash := common.BytesToHash(rec.TxHash)

		// 1. Fetch the raw transaction from the RPC node.
		rpcTx, err := rpc.GetTransactionByHash(ctx, 1, 3, txHash)
		if err != nil {
			slog.Warn("failed to fetch tx, will retry later",
				"tx", txHash.Hex(), "err", err)
			if innerErr := db.IncrementRetryCount(ctx, txHash); innerErr != nil {
				slog.Error("IncrementRetryCount", "err", innerErr)
			}
			transient++
			continue
		}

		// 2. Attempt to decode the transaction using the ABI registry.
		decoded, err := decoding.DecodeRPCTransaction(rpcTx)
		if err != nil {
			// "unknown method selector" means this tx never targeted our contract —
			// it will never decode. Abandon it immediately.
			if strings.Contains(err.Error(), "unknown method selector") ||
				strings.Contains(err.Error(), "no method selector") ||
				strings.Contains(err.Error(), "no input data") {
				reason := fmt.Sprintf("permanent: %v", err)
				if innerErr := db.MarkFailedTxAbandoned(ctx, txHash, reason); innerErr != nil {
					slog.Error("MarkFailedTxAbandoned", "err", innerErr)
				}
				slog.Debug("abandoned non-storage tx", "tx", txHash.Hex())
				abandoned++
			} else {
				// Transient ABI decode failure — bump count and try again later.
				if innerErr := db.IncrementRetryCount(ctx, txHash); innerErr != nil {
					slog.Error("IncrementRetryCount", "err", innerErr)
				}
				slog.Warn("transient decode failure, will retry",
					"tx", txHash.Hex(), "err", err)
				transient++
			}
			continue
		}

		// 3. Persist the successfully decoded transaction into the actions table.
		blockHash := common.BytesToHash(rec.BlockHash).Bytes()
		if err := db.InsertTransaction(ctx, rec.BlockNum, blockHash, decoded); err != nil {
			slog.Error("failed to insert recovered tx",
				"tx", txHash.Hex(), "err", err)
			if innerErr := db.IncrementRetryCount(ctx, txHash); innerErr != nil {
				slog.Error("IncrementRetryCount", "err", innerErr)
			}
			transient++
			continue
		}

		// 4. Mark as resolved.
		if err := db.MarkFailedTxResolved(ctx, txHash); err != nil {
			slog.Error("MarkFailedTxResolved", "err", err)
		}

		slog.Info("resolved failed tx",
			"tx", txHash.Hex(), "method", decoded.MethodName, "block", rec.BlockNum)
		resolved++
	}

	slog.Info("retry run complete",
		"resolved", resolved,
		"abandoned", abandoned,
		"transient", transient,
	)
	return nil
}
