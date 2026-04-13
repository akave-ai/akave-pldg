package decoding

import (
	"fmt"
	"time"

	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/core/types"
)

func DecodeBlock(b *types.Block) (*utils.DecodedBlock, error) {
	if b == nil {
		return nil, fmt.Errorf("nil block")
	}

	block := &utils.Block{
		Num:        int64(b.NumberU64()),
		Hash:       b.Hash().Bytes(),
		ParentHash: b.ParentHash().Bytes(),
		Timestamp:  time.Unix(int64(b.Time()), 0),
	}

	var decodedTxs []*utils.DecodedTx
	var failedTxs []*utils.FailedTx

	for _, tx := range b.Transactions() {
		from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			failedTxs = append(failedTxs, &utils.FailedTx{
				TxHash:      tx.Hash(),
				BlockNum:    int64(b.NumberU64()),
				BlockHash:   b.Hash(),
				ErrorReason: fmt.Sprintf("recover sender: %v", err),
			})
			continue
		}
		to := tx.To()
		dtx, err := DecodeTransaction(tx, from, *to)
		if err != nil {
			// Collect decode failures so they can be retried later.
			failedTxs = append(failedTxs, &utils.FailedTx{
				TxHash:      tx.Hash(),
				BlockNum:    int64(b.NumberU64()),
				BlockHash:   b.Hash(),
				ErrorReason: fmt.Sprintf("decode tx: %v", err),
			})
			continue
		}
		if dtx == nil {
			// tx not targeting our storage contract — not a failure, skip silently.
			continue
		}
		decodedTxs = append(decodedTxs, dtx)
	}

	return &utils.DecodedBlock{
		Block:     block,
		Txs:       decodedTxs,
		FailedTxs: failedTxs,
	}, nil
}
