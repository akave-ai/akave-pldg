package decoding

import (
	"data-explorer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/keccak"
	"math/big"
	"testing"
)

func TestDecodeAnyLog(t *testing.T) {
	abi := utils.GetABI()

	tests := []struct {
		name      string
		eventName string
		setupLog  func() types.Log
		check     func(*testing.T, *utils.DecodedEvent)
	}{
		{
			name:      "Decode CreateBucket",
			eventName: "CreateBucket",
			setupLog: func() types.Log {
				event := abi.Events["CreateBucket"]
				bucketId := common.Hash{1}
				var nameHash = common.Hash(keccak.NewLegacyKeccak256().Sum([]byte("test-bucket")))
				owner := common.HexToAddress("0x0000000000000000000000000000000000000000000000000000000000000123")

				topics := []common.Hash{
					event.ID,
					bucketId,
					nameHash,
					common.BytesToHash(owner.Bytes()),
				}

				return types.Log{
					Topics: topics,
					Data:   []byte{},
				}
			},
			check: func(t *testing.T, decoded *utils.DecodedEvent) {
				if decoded.EventName != "CreateBucket" {
					t.Errorf("Expected CreateBucket, got %s", decoded.EventName)
				}
				data := decoded.Data
				idVal, ok := data["id"].(string)
				if !ok || idVal != "0x0100000000000000000000000000000000000000000000000000000000000000" {
					t.Errorf("Incorrect id hex: %v", data["id"])
				}
				nameVal, ok := data["name"].(string)
				if !ok || nameVal != common.Hash(keccak.NewLegacyKeccak256().Sum([]byte("test-bucket"))).Hex() {
					t.Errorf("Incorrect name string: %v", data["name"])
				}
				ownerVal, ok := data["owner"].(common.Address)
				if !ok || ownerVal != common.HexToAddress("0x0000000000000000000000000000000000000000000000000000000000000123") {
					t.Errorf("Incorrect owner: %v", data["owner"])
				}
			},
		},
		{
			name:      "Decode FillChunkBlock",
			eventName: "FillChunkBlock",
			setupLog: func() types.Log {
				event := abi.Events["FillChunkBlock"]
				fileId := common.Hash{2}
				chunkIndex := big.NewInt(5)
				blockIndex := big.NewInt(3)

				blockCID := common.Hash{10}
				nodeId := common.Hash{20}

				topics := []common.Hash{
					event.ID,
					fileId,
					common.BigToHash(chunkIndex),
					common.BigToHash(blockIndex),
				}

				logData := make([]byte, 64)
				copy(logData[0:32], blockCID[:])
				copy(logData[32:64], nodeId[:])

				return types.Log{
					Topics: topics,
					Data:   logData,
				}
			},
			check: func(t *testing.T, decoded *utils.DecodedEvent) {
				data := decoded.Data
				if data["file_id"].(string) != "0x0200000000000000000000000000000000000000000000000000000000000000" {
					t.Errorf("Incorrect file_id")
				}
				if data["chunk_index"].(*big.Int).Cmp(big.NewInt(5)) != 0 {
					t.Errorf("Incorrect chunk_index")
				}
				if data["block_cid"].(string) != "0x0a00000000000000000000000000000000000000000000000000000000000000" {
					t.Errorf("Incorrect block_cid")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.setupLog()
			decoded, err := DecodeAnyLog(log)
			if err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}
			tt.check(t, decoded)
		})
	}
}

func TestStructToMap(t *testing.T) {
	type TestStruct struct {
		Name common.Hash `json:"name"`
		Id   common.Hash `json:"id"`
	}

	var nameHash = common.Hash(keccak.NewLegacyKeccak256().Sum([]byte("my-bucket")))
	idHash := common.HexToHash("0xabc")

	s := TestStruct{
		Name: nameHash,
		Id:   idHash,
	}

	result := structToMap("CreateBucket", s)

	if val, ok := result["name"]; !ok || val != nameHash.Hex() {
		t.Errorf("Deterministic conversion for name failed: %v", val)
	}
	if val, ok := result["id"]; !ok || val != idHash.Hex() {
		t.Errorf("Hex conversion for id failed (should not be string): %v", val)
	}
}
