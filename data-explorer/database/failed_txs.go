package database

import (
	"context"
	"fmt"
	"time"

	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
)

// FailedTxRecord mirrors a row in the failed_txs table.
type FailedTxRecord struct {
	TxHash        []byte
	BlockNum      int64
	BlockHash     []byte
	ErrorReason   string
	RetryCount    int
	Status        string
	InsertedAt    time.Time
	LastAttempted *time.Time
}

// InsertFailedTxBatch saves a batch of failed transactions.
// ON CONFLICT DO NOTHING means duplicate tx_hashes from re-indexes are silently skipped.
func (db *DB) InsertFailedTxBatch(ctx context.Context, txs []*utils.FailedTx) error {
	if len(txs) == 0 {
		return nil
	}
	query := `
		INSERT INTO failed_txs (tx_hash, block_num, block_hash, error_reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tx_hash) DO NOTHING
	`
	for _, ft := range txs {
		_, err := db.conn.ExecContext(ctx, query,
			ft.TxHash.Bytes(),
			ft.BlockNum,
			ft.BlockHash.Bytes(),
			ft.ErrorReason,
		)
		if err != nil {
			return fmt.Errorf("InsertFailedTx %s: %w", ft.TxHash.Hex(), err)
		}
	}
	return nil
}

// ListPendingFailedTxs returns up to limit rows that have not yet been resolved
// and whose retry_count is below maxRetries, ordered oldest block first.
func (db *DB) ListPendingFailedTxs(ctx context.Context, maxRetries, limit int) ([]FailedTxRecord, error) {
	query := `
		SELECT tx_hash, block_num, block_hash, error_reason, retry_count, status, inserted_at, last_attempted
		FROM failed_txs
		WHERE status = 'pending' AND retry_count < $1
		ORDER BY block_num ASC
		LIMIT $2
	`
	rows, err := db.conn.QueryContext(ctx, query, maxRetries, limit)
	if err != nil {
		return nil, fmt.Errorf("ListPendingFailedTxs: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []FailedTxRecord
	for rows.Next() {
		var r FailedTxRecord
		if err := rows.Scan(
			&r.TxHash, &r.BlockNum, &r.BlockHash,
			&r.ErrorReason, &r.RetryCount, &r.Status,
			&r.InsertedAt, &r.LastAttempted,
		); err != nil {
			return nil, fmt.Errorf("ListPendingFailedTxs scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkFailedTxResolved sets status = 'resolved' for a tx that was successfully
// decoded and inserted into the actions table on retry.
func (db *DB) MarkFailedTxResolved(ctx context.Context, txHash common.Hash) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE failed_txs SET status = 'resolved', last_attempted = now()
		WHERE tx_hash = $1
	`, txHash.Bytes())
	if err != nil {
		return fmt.Errorf("MarkFailedTxResolved %s: %w", txHash.Hex(), err)
	}
	return nil
}

// MarkFailedTxAbandoned sets status = 'abandoned' for a tx that has exceeded the
// maximum retry limit or whose error is considered permanent.
func (db *DB) MarkFailedTxAbandoned(ctx context.Context, txHash common.Hash, reason string) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE failed_txs
		SET status = 'abandoned', error_reason = $2, last_attempted = now()
		WHERE tx_hash = $1
	`, txHash.Bytes(), reason)
	if err != nil {
		return fmt.Errorf("MarkFailedTxAbandoned %s: %w", txHash.Hex(), err)
	}
	return nil
}

// IncrementRetryCount bumps retry_count and records the attempt timestamp.
func (db *DB) IncrementRetryCount(ctx context.Context, txHash common.Hash) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE failed_txs
		SET retry_count = retry_count + 1, last_attempted = now()
		WHERE tx_hash = $1
	`, txHash.Bytes())
	if err != nil {
		return fmt.Errorf("IncrementRetryCount %s: %w", txHash.Hex(), err)
	}
	return nil
}
