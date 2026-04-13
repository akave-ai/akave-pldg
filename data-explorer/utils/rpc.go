package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var errBlockRangeTooLarge = errors.New("block range too large")

// rpcHTTPClient is a shared HTTP client with a generous timeout so that
// slow or stalled RPC responses don't hang the indexer forever.
var rpcHTTPClient = &http.Client{Timeout: 500 * time.Second}

// We are performing manual rpc calls instead of using the package so as to efficiently handle eth_getLogs block range errors
// basically we will map error range to messages to decode the error and if the error was rate limit exceed we retry with full range but if the error was
// block range too large we will split the range and retry with smaller ranges until we get a successful response or we hit the minimum range threshold
// please do not change the logic of this function without understanding the above point as it is crucial for the performance of the indexer and also to avoid hitting rate limits frequently
func (r *RpcUrl) MakeRequest(ctx context.Context, method string, request_id int, max_retry int, body interface{}) (*JSONRPCResponse, error) {
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  body,
		ID:      request_id,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %v", err)
	}

	for i := 0; i < max_retry; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", r.GetUrl(), bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := rpcHTTPClient.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err // parent context cancelled — stop immediately
			}
			// Network error or timeout — retry with backoff
			fmt.Printf("RPC request failed (attempt %d/%d): %v\n", i+1, max_retry, err)
			time.Sleep(time.Duration(1<<i) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, errBlockRangeTooLarge
		}

		result := JSONRPCResponse{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		if err != nil {
			return nil, fmt.Errorf("failed to decode JSON-RPC response: %v", err)
		}
		_ = resp.Body.Close()

		if result.Error != nil {
			if result.Error.Code >= -33000 {
				// Handle rate limit error
				fmt.Printf("Rate limit exceeded, retrying... Attempt %d/%d\n", i+1, max_retry)
				time.Sleep(time.Duration(1<<i) * time.Second) // Exponential backoff
				continue
			} else if result.Error.Code == -32005 && method == "eth_getLogs" {
				// block range too large error (only for eth_getLogs), we will handle it in the calling function
				return nil, errBlockRangeTooLarge
			} else {
				// Handle other errors
				return nil, fmt.Errorf("JSON-RPC error [code %d]: %v", result.Error.Code, result.Error.Message)
			}
		}
		return &result, nil
	}
	return nil, errors.New("max retries exceeded")
}

func (r *RpcUrl) GetLogs(ctx context.Context, request_id int, max_retry int, start int, end int, address []common.Address, topics [][]string) ([]json.RawMessage, error) {
	params := []map[string]interface{}{
		{
			"fromBlock": fmt.Sprintf("0x%x", start),
			"toBlock":   fmt.Sprintf("0x%x", end),
			"address":   address,
			"topics":    topics,
		},
	}
	resp, retryErr := r.MakeRequest(ctx, "eth_getLogs", request_id, max_retry, params)
	if retryErr == nil {
		var logs []json.RawMessage
		if err := json.Unmarshal(resp.Result, &logs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal eth_getLogs result: %w", err)
		}
		return logs, nil
	}
	// Only split on "block range too large"; propagate other errors
	if !errors.Is(retryErr, errBlockRangeTooLarge) {
		return nil, retryErr
	}
	{
		// retry with smaller range by splitting the range into two halves and retrying with each half until we get a successful response or we hit the minimum range threshold
		mid := (start + end) / 2
		if mid == start || mid == end {
			return nil, fmt.Errorf("block range too large and cannot be split further: start=%d, end=%d", start, end)
		}

		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			out   []json.RawMessage
			first error
		)

		wg.Add(2)

		go func() {
			defer wg.Done()
			left, err := r.GetLogs(ctx, request_id, max_retry, start, mid, address, topics)
			if err != nil {
				first = err
				return
			} else {
				mu.Lock()
				out = append(out, left...)
				mu.Unlock()
			}
		}()

		go func() {
			defer wg.Done()
			right, err := r.GetLogs(ctx, request_id, max_retry, mid+1, end, address, topics)
			if err != nil {
				first = err
				return
			} else {
				mu.Lock()
				out = append(out, right...)
				mu.Unlock()
			}
		}()

		wg.Wait()

		if first != nil {
			return nil, first
		}

		return out, nil
	}
}

// RawLogToTypesLog converts a single eth_getLogs JSON object into types.Log.
func RawLogToTypesLog(raw json.RawMessage) (types.Log, error) {
	var log types.Log
	if err := json.Unmarshal(raw, &log); err != nil {
		return types.Log{}, fmt.Errorf("failed to unmarshal log: %w", err)
	}
	return log, nil
}

// GetBlockNumber returns the latest block number from the chain.
func (r *RpcUrl) GetBlockNumber(ctx context.Context, requestID int, maxRetry int) (uint64, error) {
	params := []interface{}{}
	resp, err := r.MakeRequest(ctx, "eth_blockNumber", requestID, maxRetry, params)
	if err != nil {
		return 0, err
	}
	var hexBlock string
	if err := json.Unmarshal(resp.Result, &hexBlock); err != nil {
		return 0, fmt.Errorf("failed to unmarshal block number: %w", err)
	}
	var blockNum uint64
	if _, err := fmt.Sscanf(hexBlock, "0x%x", &blockNum); err != nil {
		return 0, fmt.Errorf("invalid block number hex %q: %w", hexBlock, err)
	}
	return blockNum, nil
}

// eth_getBlockByNumber result (subset of fields we need).
type rpcBlockResult struct {
	Number     string `json:"number"`
	Hash       string `json:"hash"`
	ParentHash string `json:"parentHash"`
	Timestamp  string `json:"timestamp"`
}

// GetBlockByNumber fetches block metadata (hash, parentHash, timestamp) for the given block number.
func (r *RpcUrl) GetBlockByNumber(ctx context.Context, requestID int, maxRetry int, blockNum uint64) (*Block, error) {
	// "false" = do not return full transaction objects
	params := []interface{}{fmt.Sprintf("0x%x", blockNum), false}
	resp, err := r.MakeRequest(ctx, "eth_getBlockByNumber", requestID, maxRetry, params)
	if err != nil {
		return nil, fmt.Errorf("get block %d: %w", blockNum, err)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("block %d not found", blockNum)
	}
	var res rpcBlockResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	var num uint64
	if _, err := fmt.Sscanf(res.Number, "0x%x", &num); err != nil {
		return nil, fmt.Errorf("invalid block number hex %q: %w", res.Number, err)
	}
	var ts uint64
	if _, err := fmt.Sscanf(res.Timestamp, "0x%x", &ts); err != nil {
		return nil, fmt.Errorf("invalid timestamp hex %q: %w", res.Timestamp, err)
	}
	hash := common.HexToHash(res.Hash)
	parentHash := common.HexToHash(res.ParentHash)
	return &Block{
		Num:        int64(num),
		Hash:       hash.Bytes(),
		ParentHash: parentHash.Bytes(),
		Timestamp:  time.Unix(int64(ts), 0),
	}, nil
}

// GetCodeAtAddress fetches the bytecode at a given address using eth_getCode.
func (r *RpcUrl) GetCodeAtAddress(ctx context.Context, requestID int, maxRetry int, address common.Address) (string, error) {
	body := []interface{}{address.Hex(), "latest"}
	resp, err := r.MakeRequest(ctx, "eth_getCode", requestID, maxRetry, body)
	if err != nil {
		return "", fmt.Errorf("get code for address %s: %w", address.Hex(), err)
	}
	var code string
	if err := json.Unmarshal(resp.Result, &code); err != nil {
		return "", fmt.Errorf("failed to unmarshal code: %w", err)
	}
	return code, nil
}

func (r *RpcUrl) GetStorageAt(ctx context.Context, requestID int, maxRetry int, address common.Address, slot common.Hash, block string) (string, error) {
	body := []interface{}{address.Hex(), slot.Hex(), block}
	resp, err := r.MakeRequest(ctx, "eth_getStorageAt", requestID, maxRetry, body)
	if err != nil {
		return "", fmt.Errorf("get storage at address %s slot %s: %w", address.Hex(), slot.Hex(), err)
	}
	var value string
	if err := json.Unmarshal(resp.Result, &value); err != nil {
		return "", fmt.Errorf("failed to unmarshal storage value: %w", err)
	}
	return value, nil
}

func (r *RpcUrl) GetBlockAndTransactions(
	ctx context.Context,
	requestID int,
	maxRetry int,
	blockNum uint64,
) ([]*RPCTransaction, error) {
	_, txs, err := r.GetBlockWithTransactions(ctx, requestID, maxRetry, blockNum)
	return txs, err
}

// GetBlockWithTransactions fetches a block's metadata AND its full transaction
// list in a single eth_getBlockByNumber call. Use this instead of calling
// GetBlockByNumber + GetBlockAndTransactions separately.
func (r *RpcUrl) GetBlockWithTransactions(
	ctx context.Context,
	requestID int,
	maxRetry int,
	blockNum uint64,
) (*Block, []*RPCTransaction, error) {
	params := []interface{}{
		fmt.Sprintf("0x%x", blockNum),
		true, // include full transaction objects
	}

	resp, err := r.MakeRequest(ctx, "eth_getBlockByNumber", requestID, maxRetry, params)
	if err != nil {
		return nil, nil, fmt.Errorf("get block %d: %w", blockNum, err)
	}

	var raw json.RawMessage
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, fmt.Errorf("block %d not found", blockNum)
	}

	var rpcBlock RPCBlock
	if err := json.Unmarshal(raw, &rpcBlock); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	var num uint64
	if _, err := fmt.Sscanf(rpcBlock.Number, "0x%x", &num); err != nil {
		return nil, nil, fmt.Errorf("invalid block number %q: %w", rpcBlock.Number, err)
	}
	var ts uint64
	if _, err := fmt.Sscanf(rpcBlock.Timestamp, "0x%x", &ts); err != nil {
		return nil, nil, fmt.Errorf("invalid timestamp %q: %w", rpcBlock.Timestamp, err)
	}

	hash := common.HexToHash(rpcBlock.Hash)
	parentHash := common.HexToHash(rpcBlock.ParentHash)

	block := &Block{
		Num:        int64(num),
		Hash:       hash.Bytes(),
		ParentHash: parentHash.Bytes(),
		Timestamp:  time.Unix(int64(ts), 0),
	}

	return block, rpcBlock.Transactions, nil
}

func (r *RpcUrl) GetTransactionByHash(ctx context.Context, requestID int, maxRetry int, txHash common.Hash) (*RPCTransaction, error) {
	body := []interface{}{txHash.Hex()}
	resp, err := r.MakeRequest(ctx, "eth_getTransactionByHash", requestID, maxRetry, body)
	if err != nil {
		return nil, fmt.Errorf("get transaction by hash %s: %w", txHash.Hex(), err)
	}
	var tx RPCTransaction
	if err := json.Unmarshal(resp.Result, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return &tx, nil
}
