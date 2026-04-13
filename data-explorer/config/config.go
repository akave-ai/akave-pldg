package config

import (
	"fmt"

	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/common"
)

// BackfillConfig holds settings for the eth_getLogs backfill.
type BackfillConfig struct {
	RPCURL            string
	ContractAddresses []common.Address
	FromBlock         uint64
	ToBlock           uint64 // 0 means use latest
	ChunkSize         uint64 // blocks per eth_getLogs call
	MaxRetry          int
	ChainID           string // for indexing_state
	MaxRPCCalls       int    // maximum number of concurrent RPC calls
}

// DefaultBackfillConfig returns config with sensible defaults.
// It fetches the list of storage contract addresses dynamically from the explorer API.
func DefaultBackfillConfig() (BackfillConfig, error) {
	addresses, err := utils.FetchStorageContractAddresses()
	if err != nil {
		return BackfillConfig{}, fmt.Errorf("fetch storage contract addresses: %w", err)
	}
	if len(addresses) == 0 {
		return BackfillConfig{}, fmt.Errorf("no storage contract addresses returned from explorer API")
	}
	return BackfillConfig{
		RPCURL:            "https://c6-us.akave.ai/ext/bc/56g16Hr1SHQRzdM8JLm3GKYv7APVHY8T2TyeZLvDVzCaTRS7W/rpc",
		ContractAddresses: addresses,
		FromBlock:         0,
		ToBlock:           0,
		ChunkSize:         2000,
		MaxRetry:          5,
		ChainID:           "default",
		MaxRPCCalls:       8,
	}, nil
}
