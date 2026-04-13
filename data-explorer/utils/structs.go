package utils

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// HexBytes is a []byte that JSON-encodes as a "0x"-prefixed hex string
// instead of base64, matching Ethereum conventions.
type HexBytes []byte

func (h HexBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal("0x" + hex.EncodeToString(h))
}

func (h *HexBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("HexBytes: %w", err)
	}
	*h = b
	return nil
}

type RpcUrl struct {
	Url string
}

func (r *RpcUrl) GetUrl() string {
	return r.Url
}

func NewRpcUrl(url string) *RpcUrl {
	return &RpcUrl{
		Url: url,
	}
}

type Block struct {
	Num        int64
	Hash       []byte
	ParentHash []byte
	Timestamp  time.Time
}

// RPCBlock represents a full Ethereum block from JSON-RPC response
type RPCBlock struct {
	Difficulty       string            `json:"difficulty"`
	ExtraData        string            `json:"extraData"`
	GasLimit         string            `json:"gasLimit"`
	GasUsed          string            `json:"gasUsed"`
	Hash             string            `json:"hash"`
	LogsBloom        string            `json:"logsBloom"`
	Miner            string            `json:"miner"`
	MixHash          string            `json:"mixHash"`
	Nonce            string            `json:"nonce"`
	Number           string            `json:"number"`
	ParentHash       string            `json:"parentHash"`
	ReceiptsRoot     string            `json:"receiptsRoot"`
	Sha3Uncles       string            `json:"sha3Uncles"`
	Size             string            `json:"size"`
	StateRoot        string            `json:"stateRoot"`
	Timestamp        string            `json:"timestamp"`
	TotalDifficulty  string            `json:"totalDifficulty"`
	Transactions     []*RPCTransaction `json:"transactions"` // Can be []string or []RPCTransaction depending on params
	TransactionsRoot string            `json:"transactionsRoot"`
	Uncles           []string          `json:"uncles"`
}

// RPCTransaction represents a full Ethereum transaction from JSON-RPC response
type RPCTransaction struct {
	AccessList           []interface{} `json:"accessList,omitempty"` // EIP-2930 access list
	BlockHash            string        `json:"blockHash"`
	BlockNumber          string        `json:"blockNumber"`
	ChainID              string        `json:"chainId,omitempty"`
	From                 string        `json:"from"`
	Gas                  string        `json:"gas"`
	GasPrice             string        `json:"gasPrice"`
	Hash                 string        `json:"hash"`
	Input                string        `json:"input"`
	MaxFeePerGas         string        `json:"maxFeePerGas,omitempty"`         // EIP-1559
	MaxPriorityFeePerGas string        `json:"maxPriorityFeePerGas,omitempty"` // EIP-1559
	Nonce                string        `json:"nonce"`
	R                    string        `json:"r"` // Signature R
	S                    string        `json:"s"` // Signature S
	To                   string        `json:"to"`
	TransactionIndex     string        `json:"transactionIndex"`
	Type                 string        `json:"type"` // 0x0=legacy, 0x1=EIP-2930, 0x2=EIP-1559
	V                    string        `json:"v"`    // Signature V
	Value                string        `json:"value"`
	YParity              string        `json:"yParity,omitempty"` // EIP-2930/1559
}

type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *JSONRPCError   `json:"error"`
	ID      int             `json:"id"`
}

// Structs as per ABI specifications

// Event structs

type CreateBucketEvent struct {
	Id    common.Hash    `json:"id"`
	Name  common.Hash    `json:"name"`
	Owner common.Address `json:"owner"`
}

type CreateFileEvent struct {
	Id       common.Hash    `json:"id"`
	BucketId common.Hash    `json:"bucket_id"`
	Name     common.Hash    `json:"name"`
	Owner    common.Address `json:"owner"`
}

type AddFileChunkEvent struct {
	Id       common.Hash    `json:"id"`
	BucketId common.Hash    `json:"bucket_id"`
	Name     common.Hash    `json:"name"`
	Owner    common.Address `json:"owner"`
}

type CommitFileEvent struct {
	Id       common.Hash    `json:"id"`
	BucketId common.Hash    `json:"bucket_id"`
	Name     common.Hash    `json:"name"`
	Owner    common.Address `json:"owner"`
}

type FillChunkBlockEvent struct {
	FileId     common.Hash `json:"file_id"`
	ChunkIndex *big.Int    `json:"chunk_index"`
	BlockIndex *big.Int    `json:"block_index"`
	BlockCID   common.Hash `json:"block_cid"`
	NodeId     common.Hash `json:"node_id"`
}

type AddFileBlocksEvent struct {
	Ids    common.Hash `json:"ids"`
	FileId common.Hash `json:"file_id"`
}

type AddPeerBlockEvent struct {
	BlockId common.Hash `json:"block_id"`
	PeerId  common.Hash `json:"peer_id"`
}

type DeleteBucketEvent struct {
	Id    common.Hash    `json:"id"`
	Name  common.Hash    `json:"name"`
	Owner common.Address `json:"owner"`
}

type DeletePeerBlockEvent struct {
	BlockId common.Hash `json:"block_id"`
	PeerId  common.Hash `json:"peer_id"`
}

type DeleteFileEvent struct {
	Id       common.Hash    `json:"id"`
	BucketId common.Hash    `json:"bucket_id"`
	Name     common.Hash    `json:"name"`
	Owner    common.Address `json:"owner"`
}

type EIP712DomainChangedEvent struct {
}

type InitializedEvent struct {
	Version uint64 `json:"version"`
}

type UpgradedEvent struct {
	Implementation common.Address `json:"implementation"`
}

type DecodedEvent struct {
	EventName       string                 `json:"event_name"`
	ContractAddress common.Address         `json:"contract_address"`
	BlockNumber     uint64                 `json:"block_number"`
	BlockHash       common.Hash            `json:"block_hash"`
	TxHash          common.Hash            `json:"tx_hash"`
	LogIndex        uint                   `json:"log_index"`
	Topics          []common.Hash          `json:"topics"`
	Data            map[string]interface{} `json:"data"`
}

type EventMeta struct {
	Name    string
	Factory func() interface{}
}

// Transaction Structs

type DecodedTx struct {
	TxHash     common.Hash    `json:"tx_hash"`
	BlockNum   int64          `json:"block_num"` // populated during backfill
	MethodName string         `json:"method_name"`
	From       common.Address `json:"from"`
	To         common.Address `json:"to"`
	Params     interface{}    `json:"params"`
	Value      *big.Int       `json:"value"`
}

type AddFileChunkTxParams struct {
	ChunkCID         HexBytes      `json:"chunkCID"`
	BucketId         common.Hash   `json:"bucketId"`
	FileName         string        `json:"fileName"`
	EncodedChunkSize *big.Int      `json:"encodedChunkSize"`
	Cids             []common.Hash `json:"cids"`
	ChunkBlocksSizes []*big.Int    `json:"chunkBlocksSizes"`
	ChunkIndex       *big.Int      `json:"chunkIndex"`
}

type AddFileChunksTxParams struct {
	Cids               []HexBytes      `json:"cids"`
	BucketId           common.Hash     `json:"bucketId"`
	FileName           string          `json:"fileName"`
	EncodedChunkSizes  []*big.Int      `json:"encodedChunkSizes"`
	ChunkBlocksCIDs    [][]common.Hash `json:"chunkBlocksCIDs"`
	ChunkBlockSizes    [][]*big.Int    `json:"chunkBlockSizes"`
	StartingChunkIndex *big.Int        `json:"startingChunkIndex"`
}

type CommitFileTxParams struct {
	BucketId        common.Hash `json:"bucketId"`
	FileName        string      `json:"fileName"`
	EncodedFileSize *big.Int    `json:"encodedFileSize"`
	ActualSize      *big.Int    `json:"actualSize"`
	FileCID         HexBytes    `json:"fileCID"`
}

type CreateBucketTxParams struct {
	BucketName string `json:"bucketName"`
}

type CreateFileTxParams struct {
	BucketId common.Hash `json:"bucketId"`
	FileName string      `json:"fileName"`
}

type DeleteBucketTxParams struct {
	Id         common.Hash `json:"id"`
	BucketName string      `json:"bucketName"`
	Index      *big.Int    `json:"index"`
}

type DeleteFileTxParams struct {
	FileID   common.Hash `json:"fileID"`
	BucketId common.Hash `json:"bucketId"`
	FileName string      `json:"fileName"`
	Index    *big.Int    `json:"index"`
}

type FillChunkBlockArgs struct {
	BlockCID   common.Hash `json:"blockCID"`
	NodeId     common.Hash `json:"nodeId"`
	BucketId   common.Hash `json:"bucketId"`
	ChunkIndex *big.Int    `json:"chunkIndex"`
	Nonce      *big.Int    `json:"nonce"`
	BlockIndex uint8       `json:"blockIndex"`
	FileName   string      `json:"fileName"`
	Signature  HexBytes    `json:"signature"`
	Deadline   *big.Int    `json:"deadline"`
}

type FillChunkBlockTxParams struct {
	Args FillChunkBlockArgs `json:"fillChunkBlockArgs"`
}

type FillChunkBlocksTxParams struct {
	Args []FillChunkBlockArgs `json:"fillChunkBlocksArgs"`
}

type InitializeTxParams struct {
	TokenAddress common.Address `json:"tokenAddress"`
}

type SetAccessManagerTxParams struct {
	AccessManagerAddress common.Address `json:"accessManagerAddress"`
}

type SetAuthorityTxParams struct {
	UpgradeAuthority common.Address `json:"_upgradeAuthority"`
}

type UpgradeToAndCallTxParams struct {
	NewImplementation common.Address `json:"newImplementation"`
	Data              HexBytes       `json:"data"`
}

// FailedTx records a transaction that could not be decoded during block
// processing. It is persisted to the failed_txs table so the tx can be
// retried later without re-scanning the entire block range.
type FailedTx struct {
	TxHash      common.Hash `json:"tx_hash"`
	BlockNum    int64       `json:"block_num"`
	BlockHash   common.Hash `json:"block_hash"`
	ErrorReason string      `json:"error_reason"`
}

// DecodedBlock represents a chain block plus all successfully decoded
// contract transactions within it, and any transactions that failed to decode.
type DecodedBlock struct {
	Block     *Block       `json:"block"`
	Txs       []*DecodedTx `json:"txs"`
	FailedTxs []*FailedTx  `json:"failed_txs,omitempty"`
}
