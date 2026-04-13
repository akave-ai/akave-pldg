package decoding

import (
	"math/big"
	"testing"

	storage "data-explorer/integration/contracts"
	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
)

// setupSimulatedStorage deploys the storage contract on a simulated backend (same pattern as integration/setup_contracts_test.go).
func setupSimulatedStorage(t *testing.T) (*simulated.Backend, *storage.Storage, common.Address, *bind.TransactOpts) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	chainID := big.NewInt(1337)
	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		t.Fatal(err)
	}
	balance := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	alloc := types.GenesisAlloc{auth.From: {Balance: balance}}
	sim := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(8000000))

	contractAddr, _, instance, err := storage.DeployStorage(auth, sim.Client())
	if err != nil {
		t.Fatalf("deploy storage: %v", err)
	}
	sim.Commit()
	return sim, instance, contractAddr, auth
}

func TestDecodeTransaction(t *testing.T) {
	_, instance, contractAddress, auth := setupSimulatedStorage(t)

	tests := []struct {
		name      string
		setupTx   func() *types.Transaction
		expectErr bool
		check     func(*testing.T, *utils.DecodedTx)
	}{
		{
			name: "Success - CreateBucket (storage binding)",
			setupTx: func() *types.Transaction {
				tx, err := instance.CreateBucket(auth, "test-bucket")
				if err != nil {
					t.Fatalf("CreateBucket: %v", err)
				}
				return tx
			},
			expectErr: false,
			check: func(t *testing.T, decoded *utils.DecodedTx) {
				if decoded == nil {
					t.Fatal("expected decoded tx, got nil")
					return
				}
				if decoded.MethodName != "createBucket" {
					t.Errorf("expected createBucket, got %s", decoded.MethodName)
				}
				if decoded.From != auth.From {
					t.Errorf("expected from %s, got %s", auth.From.Hex(), decoded.From.Hex())
				}
				if decoded.To != contractAddress {
					t.Errorf("expected to %s, got %s", contractAddress.Hex(), decoded.To.Hex())
				}
				params, ok := decoded.Params.(utils.CreateBucketTxParams)
				if !ok {
					t.Fatalf("expected CreateBucketTxParams, got %T", decoded.Params)
				}
				if params.BucketName != "test-bucket" {
					t.Errorf("expected bucketName test-bucket, got %v", params.BucketName)
				}
			},
		},
		{
			name: "Wrong contract address (unknown selector)",
			setupTx: func() *types.Transaction {
				key, _ := crypto.GenerateKey()
				signer := types.LatestSignerForChainID(big.NewInt(1337))
				tx := types.NewTransaction(0, common.HexToAddress("0x123"), big.NewInt(0), 100000, big.NewInt(1), []byte{1, 2, 3, 4})
				signed, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatal(err)
				}
				return signed
			},
			expectErr: true,
			check: func(t *testing.T, decoded *utils.DecodedTx) {
				if decoded != nil {
					t.Errorf("expected nil, got %v", decoded)
				}
			},
		},
		{
			name: "No method selector",
			setupTx: func() *types.Transaction {
				key, _ := crypto.GenerateKey()
				signer := types.LatestSignerForChainID(big.NewInt(1337))
				tx := types.NewTransaction(0, contractAddress, big.NewInt(0), 100000, big.NewInt(1), []byte{1, 2, 3})
				signed, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatal(err)
				}
				return signed
			},
			expectErr: true,
			check: func(t *testing.T, decoded *utils.DecodedTx) {
				if decoded != nil {
					t.Errorf("expected nil, got %v", decoded)
				}
			},
		},
		{
			name: "Unknown method",
			setupTx: func() *types.Transaction {
				key, _ := crypto.GenerateKey()
				signer := types.LatestSignerForChainID(big.NewInt(1337))
				tx := types.NewTransaction(0, contractAddress, big.NewInt(0), 100000, big.NewInt(1), []byte{1, 2, 3, 4, 5})
				signed, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatal(err)
				}
				return signed
			},
			expectErr: true,
			check: func(t *testing.T, decoded *utils.DecodedTx) {
				if decoded != nil {
					t.Errorf("expected nil, got %v", decoded)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := tt.setupTx()
			from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				t.Fatal(err)
			}
			toAddress := tx.To()
			if toAddress == nil {
				t.Fatal("to address is nil")
				return
			}
			decoded, err := DecodeTransaction(tx, from, *toAddress)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
			}
			if tt.check != nil {
				tt.check(t, decoded)
			}
		})
	}
}
