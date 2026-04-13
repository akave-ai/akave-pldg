package integration

import (
	"context"
	"testing"

	"data-explorer/decoding"
	"data-explorer/indexing"
	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Known Akave testnet block used for deterministic integration tests (https://explorer.akave.ai/block/1370734).
const testBlockNumber = 1370734

// https://explorer.akave.ai/tx/0x11ff40024763f2f161f76330a2a29ea00fedf13466d03bd97283e7a3c633fbf7
var testTransactionHashes = []string{"0x11ff40024763f2f161f76330a2a29ea00fedf13466d03bd97283e7a3c633fbf7", "0x6e835f48c8cc45c9bce02f1204f9fb232cfb44557c99a946ac29d88b3abe8ebc", "0xe7ad1f254397115c3eafa9319b40aab855b76d424f393943a78f65dc618d167f"}

// setupTestTransaction builds signed tx that target the storage contract and decode as
func setupTestTransactions(t *testing.T, client *ethclient.Client) []*types.Transaction {
	t.Helper()
	testTx, isPending, err := client.TransactionByHash(context.Background(), common.HexToHash(testTransactionHashes[0]))
	if err != nil {
		t.Fatalf("Failed to get test transaction: %v", err)
	}
	if isPending {
		t.Fatalf("Test transaction is pending")
	}

	receipt, err := client.TransactionReceipt(context.Background(), testTx.Hash())
	if err != nil {
		t.Fatalf("Failed to get test transaction receipt: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("Test transaction failed")
	}

	return []*types.Transaction{testTx}
}

// TestDecodeTestTransactions decodes the tx from the known test transaction
func TestDecodeTestTransactions(t *testing.T) {
	client := setupClient(t)
	testTxs := setupTestTransactions(t, client)
	for _, testTx := range testTxs {
		t.Logf("test tx: hash=%s to=%s", testTx.Hash().Hex(), (testTx.To()).Hex())

		from, err := types.Sender(types.LatestSignerForChainID(testTx.ChainId()), testTx)
		if err != nil {
			t.Fatalf("Failed to get sender: %v", err)
		}
		toAddress := testTx.To()
		if toAddress == nil {
			t.Fatalf("Failed to get to address: %v", err)
			return
		}
		decoded, err := decoding.DecodeTransaction(testTx, from, *toAddress)
		if err != nil {
			t.Fatalf("Failed to decode transaction: %v", err)
		}
		t.Logf("decoded: method=%s from=%s", decoded.MethodName, decoded.From.Hex())
	}
}

func setupClient(t *testing.T) *ethclient.Client {
	t.Helper()
	client, err := ethclient.Dial(utils.GetRPCURL())
	if err != nil {
		t.Fatalf("Failed to connect to Akave RPC: %v", err)
	}
	return client
}

func getKnownRange(t *testing.T, client *ethclient.Client) (int64, int64) {
	t.Helper()
	head, err := client.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("Failed to get latest block: %v", err)
	}
	to := int64(head) - 100
	from := to - 10
	if from < 0 {
		from = 0
	}
	return from, to
}

func TestBackfillDeterministicRange(t *testing.T) {
	client := setupClient(t)
	from, to := getKnownRange(t, client)

	events, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("FetchAndDecode failed: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("Expected events in range [%d, %d], got 0", from, to)
	}

	allowedEvents := map[string]bool{
		"CreateBucket":        true,
		"CreateFile":          true,
		"AddFileChunk":        true,
		"CommitFile":          true,
		"FillChunkBlock":      true,
		"AddFileBlocks":       true,
		"AddPeerBlock":        true,
		"DeleteBucket":        true,
		"DeletePeerBlock":     true,
		"DeleteFile":          true,
		"Initialized":         true,
		"Upgraded":            true,
		"EIP712DomainChanged": true,
	}

	for i, ev := range events {
		if !allowedEvents[ev.EventName] {
			t.Errorf("Event %d: unexpected event type %q", i, ev.EventName)
		}
		if ev.BlockNumber < uint64(from) || ev.BlockNumber > uint64(to) {
			t.Errorf("Event %d: block %d outside range [%d, %d]", i, ev.BlockNumber, from, to)
		}
		if ev.EventName == "" {
			t.Errorf("Event %d: empty event name", i)
		}
		if ev.Data == nil {
			t.Errorf("Event %d: nil data map", i)
		}
		if ev.TxHash == (common.Hash{}) {
			t.Errorf("Event %d: zero TxHash", i)
		}
		if ev.ContractAddress == (common.Address{}) {
			t.Errorf("Event %d: zero ContractAddress", i)
		}

		if i < 50 || i > len(events)-10 {
			t.Logf("[%d] LogIndex: %d, Event: %s, Block: %d, TX: %s", i, ev.LogIndex, ev.EventName, ev.BlockNumber, ev.TxHash.Hex())
		} else if i == 50 {
			t.Logf("... skipping middle events ...")
		}
	}
}

func TestBackfillBlockOrdering(t *testing.T) {
	client := setupClient(t)
	from, to := getKnownRange(t, client)

	events, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("FetchAndDecode failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("No events returned for ordering check")
	}

	for i := 1; i < len(events); i++ {
		if events[i].BlockNumber < events[i-1].BlockNumber {
			t.Errorf("Out of order at index %d: block %d < block %d",
				i, events[i].BlockNumber, events[i-1].BlockNumber)
		}

		if events[i].BlockNumber == events[i-1].BlockNumber &&
			events[i].LogIndex < events[i-1].LogIndex {
			t.Errorf("Log index out of order at index %d: logIndex %d < %d in same block %d",
				i, events[i].LogIndex, events[i-1].LogIndex, events[i].BlockNumber)
		}
	}
}

func TestBackfillBoundaryPrecision(t *testing.T) {
	client := setupClient(t)
	from, to := getKnownRange(t, client)

	blockA := to
	blockB := to - 1

	eventsA, err := indexing.FetchAndDecode(client, blockA, blockA)
	if err != nil {
		t.Fatalf("Single-block fetch for block %d failed: %v", blockA, err)
	}
	for i, ev := range eventsA {
		if ev.BlockNumber != uint64(blockA) {
			t.Errorf("Block A event %d: expected block %d, got %d", i, blockA, ev.BlockNumber)
		}
	}

	eventsB, err := indexing.FetchAndDecode(client, blockB, blockB)
	if err != nil {
		t.Fatalf("Single-block fetch for block %d failed: %v", blockB, err)
	}
	for i, ev := range eventsB {
		if ev.BlockNumber != uint64(blockB) {
			t.Errorf("Block B event %d: expected block %d, got %d", i, blockB, ev.BlockNumber)
		}
	}

	combined, err := indexing.FetchAndDecode(client, blockB, blockA)
	if err != nil {
		t.Fatalf("Range fetch [%d, %d] failed: %v", blockB, blockA, err)
	}

	expectedCount := len(eventsA) + len(eventsB)
	if len(combined) != expectedCount {
		t.Errorf("Boundary sum mismatch: block %d (%d) + block %d (%d) = %d, but range returned %d",
			blockB, len(eventsB), blockA, len(eventsA), expectedCount, len(combined))
	}

	fullRange, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("Full range fetch failed: %v", err)
	}
	subA, err := indexing.FetchAndDecode(client, from, from+5)
	if err != nil {
		t.Fatalf("Sub-range A fetch failed: %v", err)
	}
	subB, err := indexing.FetchAndDecode(client, from+6, to)
	if err != nil {
		t.Fatalf("Sub-range B fetch failed: %v", err)
	}
	if len(subA)+len(subB) != len(fullRange) {
		t.Errorf("Split range mismatch: [%d,%d](%d) + [%d,%d](%d) = %d, full range = %d",
			from, from+5, len(subA), from+6, to, len(subB),
			len(subA)+len(subB), len(fullRange))
	}
}

func TestBackfillBatchPagination(t *testing.T) {
	client := setupClient(t)
	from, to := getKnownRange(t, client)

	allAtOnce, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("Single fetch failed: %v", err)
	}

	batchSizes := []int64{1, 2, 3, 5, 10}
	for _, bs := range batchSizes {
		batched, err := indexing.FetchAndDecodeInBatches(client, from, to, bs)
		if err != nil {
			t.Fatalf("Batch fetch (size=%d) failed: %v", bs, err)
		}
		if len(batched) != len(allAtOnce) {
			t.Errorf("Batch size %d: got %d events, expected %d", bs, len(batched), len(allAtOnce))
			continue
		}
		for i := range allAtOnce {
			if batched[i].EventName != allAtOnce[i].EventName {
				t.Errorf("Batch size %d, index %d: event name %q != %q",
					bs, i, batched[i].EventName, allAtOnce[i].EventName)
				break
			}
			if batched[i].BlockNumber != allAtOnce[i].BlockNumber {
				t.Errorf("Batch size %d, index %d: block %d != %d",
					bs, i, batched[i].BlockNumber, allAtOnce[i].BlockNumber)
				break
			}
			if batched[i].TxHash != allAtOnce[i].TxHash {
				t.Errorf("Batch size %d, index %d: TxHash mismatch", bs, i)
				break
			}
			if batched[i].LogIndex != allAtOnce[i].LogIndex {
				t.Errorf("Batch size %d, index %d: LogIndex %d != %d",
					bs, i, batched[i].LogIndex, allAtOnce[i].LogIndex)
				break
			}
		}
	}
}

func TestBackfillIdempotency(t *testing.T) {
	client := setupClient(t)
	from, to := getKnownRange(t, client)

	first, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}
	second, err := indexing.FetchAndDecode(client, from, to)
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("Idempotency failed: first=%d, second=%d", len(first), len(second))
	}

	for i := range first {
		if first[i].EventName != second[i].EventName {
			t.Errorf("Index %d: event name %q != %q", i, first[i].EventName, second[i].EventName)
		}
		if first[i].BlockNumber != second[i].BlockNumber {
			t.Errorf("Index %d: block %d != %d", i, first[i].BlockNumber, second[i].BlockNumber)
		}
		if first[i].TxHash != second[i].TxHash {
			t.Errorf("Index %d: TxHash mismatch", i)
		}
		if first[i].LogIndex != second[i].LogIndex {
			t.Errorf("Index %d: LogIndex %d != %d", i, first[i].LogIndex, second[i].LogIndex)
		}
	}
}
