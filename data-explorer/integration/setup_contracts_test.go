package integration

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"data-explorer/decoding"
	storage "data-explorer/integration/contracts"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
)

func setupTest() (*simulated.Backend, *storage.Storage, *bind.TransactOpts) {
	// 1. Setup account and auth
	key, _ := crypto.GenerateKey()
	auth, _ := bind.NewKeyedTransactorWithChainID(key, big.NewInt(1337))

	// 2. Allocate funds (10 ETH)
	balance := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	alloc := types.GenesisAlloc{
		auth.From: {Balance: balance},
	}

	sim := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(8000000))

	_, _, instance, err := storage.DeployStorage(auth, sim.Client())
	if err != nil {
		panic(fmt.Sprintf("failed to deploy contract: %v", err))
	}

	sim.Commit()

	return sim, instance, auth
}

func TestContractFunctionality(t *testing.T) {
	sim, instance, auth := setupTest()

	bucketName := "test-bucket"
	expectedNameHash := crypto.Keccak256Hash([]byte(bucketName))

	// Call a contract function (e.g., createBucket)
	tx, err := instance.CreateBucket(auth, bucketName)
	if err != nil {
		t.Fatalf("failed to call CreateBucket: %v", err)
	}

	sim.Commit()

	// Check transaction receipt
	receipt, err := sim.Client().TransactionReceipt(context.Background(), tx.Hash())
	if err != nil {
		t.Fatalf("failed to get transaction receipt: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("transaction failed with status: %d", receipt.Status)
	}

	for _, lg := range receipt.Logs {
		event, err := decoding.DecodeAnyLog(*lg)
		if err == nil && event.EventName == "CreateBucket" {
			nameHash := common.HexToHash((event.Data["name"].(string)))
			if nameHash != expectedNameHash {
				t.Fatalf("bucket name hash mismatch: expected %s, got %s", expectedNameHash.Hex(), nameHash.Hex())
			}
			fmt.Println("CreateBucket event caught, name hash matches:", nameHash.Hex())
		}
	}
}
