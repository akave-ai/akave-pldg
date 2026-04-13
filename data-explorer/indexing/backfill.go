package indexing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"data-explorer/config"
	"data-explorer/decoding"
	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
)

// EventHandler is called for each decoded event during backfill.
type EventHandler func(ctx context.Context, ev *utils.DecodedEvent) error

// BatchEventHandler is called once per chunk with:
//   - events:      all decoded contract events (logs) in the chunk
//   - decodedTxs:  all successfully decoded contract transactions in those blocks
//   - failedTxs:   transactions that could not be decoded (saved for later retry)
//   - chunkEndBlock: last block number in the chunk (use for indexing_state)
//   - blocks:      metadata for every block in the chunk
type BatchEventHandler func(
	ctx context.Context,
	events []*utils.DecodedEvent,
	decodedTxs []*utils.DecodedTx,
	failedTxs []*utils.FailedTx,
	chunkEndBlock int64,
	blocks []*utils.Block,
) error

// Backfill runs eth_getLogs with event topic filters over the block range,
// decodes each log, and invokes the handler. Uses chunked requests and
// automatic range splitting for large responses.
// If batchHandler is non-nil, events are batched per chunk and passed to it; otherwise handler is called per event.
func Backfill(ctx context.Context, cfg config.BackfillConfig, handler EventHandler, batchHandler BatchEventHandler) error {
	rpc := utils.NewRpcUrl(cfg.RPCURL)
	topics := utils.EventTopicFilters()

	toBlock := cfg.ToBlock
	if toBlock == 0 {
		latest, err := rpc.GetBlockNumber(ctx, 1, cfg.MaxRetry)
		if err != nil {
			return fmt.Errorf("get latest block: %w", err)
		}
		toBlock = latest
		slog.Info("backfill using latest block", "block", latest)
	}

	from := int(cfg.FromBlock)
	to := int(toBlock)
	if from > to {
		return fmt.Errorf("fromBlock %d > toBlock %d", from, to)
	}

	chunkSize := int(cfg.ChunkSize)
	if chunkSize <= 0 {
		chunkSize = 2000
	}

	slog.Info("backfill started",
		"from", from, "to", to, "chunkSize", chunkSize, "contractAddrs", len(cfg.ContractAddresses))

	for start := from; start <= to; start += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := start + chunkSize - 1
		if end > to {
			end = to
		}

		rawLogs, err := rpc.GetLogs(ctx, 1, cfg.MaxRetry, start, end, cfg.ContractAddresses, topics)
		if err != nil {
			return fmt.Errorf("GetLogs %d-%d: %w", start, end, err)
		}

		var batch []*utils.DecodedEvent
		for i, raw := range rawLogs {
			vLog, err := utils.RawLogToTypesLog(raw)
			if err != nil {
				return fmt.Errorf("parse log %d in block range %d-%d: %w", i, start, end, err)
			}

			decoded, err := decoding.DecodeAnyLog(vLog)
			if err != nil {
				slog.Debug("skip non-storage log", "tx", vLog.TxHash, "topics", len(vLog.Topics), "err", err)
				continue
			}

			if batchHandler != nil {
				batch = append(batch, decoded)
			} else if handler != nil {
				if err := handler(ctx, decoded); err != nil {
					return fmt.Errorf("handler for %s block %d tx %s: %w",
						decoded.EventName, decoded.BlockNumber, decoded.TxHash, err)
				}
			}
		}

		if batchHandler != nil {
			var blocks []*utils.Block
			var decodedTxs []*utils.DecodedTx
			var failedTxs []*utils.FailedTx

			// Wrapped in a closure so defer cancel() fires at end of each
			// chunk iteration, not at the end of the entire Backfill call.
			err := func() error {
				// Always collect ALL block numbers in the chunk range,
				// not just blocks that contained matching events.
				blockNums := make(map[uint64]struct{})
				for num := uint64(start); num <= uint64(end); num++ {
					blockNums[num] = struct{}{}
				}

				blocks = make([]*utils.Block, 0, len(blockNums))

				workers := int(cfg.MaxRPCCalls)
				if workers <= 0 {
					workers = 8
				}

				chunkCtx, cancel := context.WithCancel(ctx)
				defer cancel() // ✅ fires at end of each chunk, not end of Backfill

				var (
					wg       sync.WaitGroup
					mu       sync.Mutex
					firstErr error
				)

				numCh := make(chan uint64)
				workerFn := func() {
					defer wg.Done()
					for num := range numCh {
						if chunkCtx.Err() != nil {
							return
						}

						// Single RPC call: fetch block metadata AND full tx list.
						b, rpcTxs, err := rpc.GetBlockWithTransactions(chunkCtx, 1, cfg.MaxRetry, num)
						if err != nil {
							mu.Lock()
							if firstErr == nil {
								firstErr = fmt.Errorf("get block %d: %w", num, err)
								cancel()
							}
							mu.Unlock()
							return
						}

						blockHash := common.BytesToHash(b.Hash)

						// Decode every transaction in this block.
						localDecoded := make([]*utils.DecodedTx, 0, len(rpcTxs))
						localFailed := make([]*utils.FailedTx, 0)
						for _, rpcTx := range rpcTxs {
							dtx, err := decoding.DecodeRPCTransaction(rpcTx)
							if err != nil {
								if isPermanentDecodeError(err) {
									// tx does not target our contract — skip silently.
									continue
								}
								// Real decode failure — record for retry.
								localFailed = append(localFailed, &utils.FailedTx{
									TxHash:      common.HexToHash(rpcTx.Hash),
									BlockNum:    b.Num,
									BlockHash:   blockHash,
									ErrorReason: fmt.Sprintf("decode tx: %v", err),
								})
								continue
							}
							dtx.BlockNum = b.Num
							localDecoded = append(localDecoded, dtx)
						}

						mu.Lock()
						blocks = append(blocks, b)
						decodedTxs = append(decodedTxs, localDecoded...)
						failedTxs = append(failedTxs, localFailed...)
						mu.Unlock()
					}
				}

				wg.Add(workers)
				for i := 0; i < workers; i++ {
					go workerFn()
				}

			outer:
				for num := range blockNums {
					select {
					case numCh <- num:
					case <-chunkCtx.Done():
						break outer
					}
				}
				close(numCh)

				wg.Wait()
				return firstErr
			}()

			if err != nil {
				return err
			}

			if err := batchHandler(ctx, batch, decodedTxs, failedTxs, int64(end), blocks); err != nil {
				return fmt.Errorf("batch handler for blocks %d-%d: %w", start, end, err)
			}
		}

		slog.Info("backfill chunk done", "from", start, "to", end, "logs", len(rawLogs))
	}

	slog.Info("backfill completed", "from", from, "to", to)
	return nil
}

// LiveIndexing continuously polls for new blocks starting from lastBlock+1,
// reusing the same contract address list fetched once at startup.
// It polls every 5 seconds when caught up, and processes new blocks as they arrive.
func LiveIndexing(ctx context.Context, rpcURL string, handler EventHandler, batchHandler BatchEventHandler, lastBlock int, chunkSize int) error {
	contractAddresses, err := utils.FetchStorageContractAddresses()
	if err != nil {
		return fmt.Errorf("live indexing: fetch contract addresses: %w", err)
	}

	rpc := utils.NewRpcUrl(rpcURL)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		latest, err := rpc.GetBlockNumber(ctx, 1, 3)
		if err != nil {
			slog.Warn("live indexing: failed to get latest block, retrying", "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if uint64(lastBlock) >= latest {
			// Caught up — wait for a new block.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}

		cfg := config.BackfillConfig{
			RPCURL:            rpcURL,
			FromBlock:         uint64(lastBlock + 1),
			ToBlock:           latest,
			ChunkSize:         uint64(chunkSize),
			MaxRetry:          3,
			ContractAddresses: contractAddresses,
		}

		slog.Info("live indexing: processing new blocks", "from", lastBlock+1, "to", latest)
		if err := Backfill(ctx, cfg, handler, batchHandler); err != nil {
			return fmt.Errorf("live indexing: backfill %d-%d: %w", lastBlock+1, latest, err)
		}

		lastBlock = int(latest)
	}
}

// NoOpHandler is a no-op EventHandler for testing or dry runs.
func NoOpHandler(ctx context.Context, ev *utils.DecodedEvent) error {
	return nil
}

// LoggingHandler logs each decoded event and passes through.
func LoggingHandler(ctx context.Context, ev *utils.DecodedEvent) error {
	slog.Info("event",
		"name", ev.EventName,
		"block", ev.BlockNumber,
		"tx", ev.TxHash,
		"logIndex", ev.LogIndex,
		"data", ev.Data)
	return nil
}

// LoggingBatchHandler logs events, decoded txs, and failed txs in the batch.
func LoggingBatchHandler(ctx context.Context, events []*utils.DecodedEvent, decodedTxs []*utils.DecodedTx, failedTxs []*utils.FailedTx, _ int64, _ []*utils.Block) error {
	for _, ev := range events {
		slog.Info("event",
			"name", ev.EventName,
			"block", ev.BlockNumber,
			"tx", ev.TxHash,
			"logIndex", ev.LogIndex,
			"data", ev.Data)
	}
	for _, tx := range decodedTxs {
		slog.Info("decoded tx",
			"method", tx.MethodName,
			"from", tx.From,
			"tx", tx.TxHash)
	}
	for _, ft := range failedTxs {
		slog.Warn("failed tx",
			"tx", ft.TxHash,
			"block", ft.BlockNum,
			"reason", ft.ErrorReason)
	}
	return nil
}

// isPermanentDecodeError returns true when a tx decode error means the transaction
// was never targeting our storage contract and should be silently skipped.
func isPermanentDecodeError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unknown method selector") ||
		strings.Contains(msg, "no method selector") ||
		strings.Contains(msg, "no input data")
}