package decoding

import (
	"data-explorer/utils"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type txMeta struct {
	name    string
	factory func() interface{}
}

var (
	txRegistry map[[4]byte]txMeta
)

func init() {
	txRegistry = make(map[[4]byte]txMeta)
	contractABI = utils.GetABI()

	factories := map[string]func() interface{}{
		"addFileChunk":     func() interface{} { return &utils.AddFileChunkTxParams{} },
		"addFileChunks":    func() interface{} { return &utils.AddFileChunksTxParams{} },
		"commitFile":       func() interface{} { return &utils.CommitFileTxParams{} },
		"createBucket":     func() interface{} { return &utils.CreateBucketTxParams{} },
		"createFile":       func() interface{} { return &utils.CreateFileTxParams{} },
		"deleteBucket":     func() interface{} { return &utils.DeleteBucketTxParams{} },
		"deleteFile":       func() interface{} { return &utils.DeleteFileTxParams{} },
		"fillChunkBlock":   func() interface{} { return &utils.FillChunkBlockTxParams{} },
		"fillChunkBlocks":  func() interface{} { return &utils.FillChunkBlocksTxParams{} },
		"initialize":       func() interface{} { return &utils.InitializeTxParams{} },
		"setAccessManager": func() interface{} { return &utils.SetAccessManagerTxParams{} },
		"setAuthority":     func() interface{} { return &utils.SetAuthorityTxParams{} },
		"upgradeToAndCall": func() interface{} { return &utils.UpgradeToAndCallTxParams{} },
	}

	for name, method := range contractABI.Methods {
		if factory, ok := factories[name]; ok {
			var selector [4]byte
			copy(selector[:], method.ID)
			txRegistry[selector] = txMeta{
				name:    name,
				factory: factory,
			}
		}
	}
}

func DecodeTransaction(tx *types.Transaction, from common.Address, to common.Address) (*utils.DecodedTx, error) {
	txData := tx.Data()
	if len(txData) < 4 {
		return nil, fmt.Errorf("no method selector")
	}

	if tx.ChainId() == nil {
		return nil, fmt.Errorf("transaction has no chain ID")
	}

	var selector [4]byte
	copy(selector[:], txData[:4])

	meta, ok := txRegistry[selector]
	if !ok {
		return nil, fmt.Errorf("unknown method")
	}

	method, err := contractABI.MethodById(selector[:])
	if err != nil {
		return nil, fmt.Errorf("failed to get method: %w", err)
	}

	values, err := method.Inputs.Unpack(txData[4:])
	if err != nil {
		return nil, fmt.Errorf("failed to unpack %s inputs: %w", meta.name, err)
	}

	params := meta.factory()
	if err := method.Inputs.Copy(params, values); err != nil {
		return nil, fmt.Errorf("failed to copy into %s params: %w", meta.name, err)
	}
	args := reflect.ValueOf(params).Elem().Interface()

	return &utils.DecodedTx{
		TxHash:     tx.Hash(),
		MethodName: meta.name,
		From:       from,
		To:         to,
		Params:     args,
		Value:      tx.Value(),
	}, nil
}

// DecodeRPCTransaction decodes an RPCTransaction (returned by eth_getTransactionByHash)
// using the same ABI registry as DecodeTransaction. Used by the failed-tx retry loop
// where we only have the JSON RPC representation, not a types.Transaction.
func DecodeRPCTransaction(rpcTx *utils.RPCTransaction) (*utils.DecodedTx, error) {
	// Parse input data from hex string.
	inputHex := rpcTx.Input
	if len(inputHex) < 2 {
		return nil, fmt.Errorf("no input data")
	}
	if inputHex[:2] == "0x" || inputHex[:2] == "0X" {
		inputHex = inputHex[2:]
	}
	txData := make([]byte, len(inputHex)/2)
	if _, err := fmt.Sscanf(inputHex, "%x", &txData); err != nil {
		// Fallback: manual hex decode
		for i := 0; i < len(inputHex)-1; i += 2 {
			var b byte
			_, err := fmt.Sscanf(inputHex[i:i+2], "%02x", &b)
			if err != nil {
				return nil, fmt.Errorf("failed to scan hex data at position %d: %w", i, err)
			}
			txData[i/2] = b
		}
	}
	if len(txData) < 4 {
		return nil, fmt.Errorf("no method selector")
	}

	var selector [4]byte
	copy(selector[:], txData[:4])

	meta, ok := txRegistry[selector]
	if !ok {
		return nil, fmt.Errorf("unknown method selector 0x%x", selector)
	}

	method, err := contractABI.MethodById(selector[:])
	if err != nil {
		return nil, fmt.Errorf("failed to get method: %w", err)
	}

	values, err := method.Inputs.Unpack(txData[4:])
	if err != nil {
		return nil, fmt.Errorf("failed to unpack %s inputs: %w", meta.name, err)
	}

	params := meta.factory()
	if err := method.Inputs.Copy(params, values); err != nil {
		return nil, fmt.Errorf("failed to copy into %s params: %w", meta.name, err)
	}
	args := reflect.ValueOf(params).Elem().Interface()

	from := common.HexToAddress(rpcTx.From)
	to := common.HexToAddress(rpcTx.To)
	txHash := common.HexToHash(rpcTx.Hash)

	var value *big.Int
	if rpcTx.Value != "" && rpcTx.Value != "0x0" {
		value = new(big.Int)
		value.SetString(strings.TrimPrefix(rpcTx.Value, "0x"), 16)
	}

	return &utils.DecodedTx{
		TxHash:     txHash,
		MethodName: meta.name,
		From:       from,
		To:         to,
		Params:     args,
		Value:      value,
	}, nil
}
