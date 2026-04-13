package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func GetLogs(t *testing.T, start int, end int) {
	ctx := context.Background()
	// Example parameters for GetLogs
	request_id := 1
	max_retry := 3
	address, err := FetchStorageContractAddresses()
	if err != nil {
		t.Fatalf("Failed to fetch storage contract addresses: %v", err)
	}
	topics := [][]string{}
	logs, err := rpc.GetLogs(ctx, request_id, max_retry, start, end, address, topics)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	var un_logs []interface{}
	for _, log := range logs {
		var un_log interface{}
		un_res := json.Unmarshal(log, &un_log)
		if un_res != nil {
			t.Fatalf("Failed to unmarshal log: %v", un_res)
		}
		un_logs = append(un_logs, un_log)
	}
	fmt.Printf("Logs: %v\n", len(un_logs))
}

func TestMakeRequest(t *testing.T) {
	//eth_chainId
	ctx := context.Background()
	response, err := rpc.MakeRequest(ctx, "eth_chainId", 1, 3, nil)
	if err != nil {
		t.Fatalf("MakeRequest failed: %v", err)
	}
	var result interface{}
	un_res := json.Unmarshal(response.Result, &result)
	if un_res != nil {
		t.Fatalf("Failed to unmarshal response: %v", un_res)
	}
	fmt.Printf("Response: %v\n", result)
}

func TestGetLogs(t *testing.T) {
	cases := []struct {
		name  string
		start int
		end   int
	}{
		{"Test Case 1", 1278075, 1278076},
		{"Test Case 2", 1256076, 1258076},
	}

	for _, c := range cases {
		before := time.Now()
		t.Run(c.name, func(t *testing.T) {
			GetLogs(t, c.start, c.end)
		})
		after := time.Now()
		fmt.Printf("%s took %v\n", c.name, after.Sub(before))
	}
}

func TestGetBlockNumber(t *testing.T) {
	ctx := context.Background()
	blockNumber, err := rpc.GetBlockNumber(ctx, 1, 3)
	if err != nil {
		t.Fatalf("GetBlockNumber failed: %v", err)
	}
	fmt.Printf("Latest Block Number: %d\n", blockNumber)
}

func TestBlockAndTxns(t *testing.T) {
	ctx := context.Background()
	txns, err := rpc.GetBlockAndTransactions(ctx, 1, 3, 1278075)
	if err != nil {
		t.Fatalf("GetBlockAndTransactions failed: %v", err)
	}
	fmt.Printf("Transactions in Block 1278075: %d\n", len(txns))
}
