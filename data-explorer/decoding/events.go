package decoding

import (
	"fmt"
	"reflect"
	"strings"

	"data-explorer/utils"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	eventRegistry map[common.Hash]utils.EventMeta
	contractABI   abi.ABI
)

func init() {
	eventRegistry = make(map[common.Hash]utils.EventMeta)
	contractABI = utils.GetABI()

	factories := map[string]func() interface{}{
		"CreateBucket":        func() interface{} { return &utils.CreateBucketEvent{} },
		"CreateFile":          func() interface{} { return &utils.CreateFileEvent{} },
		"AddFileChunk":        func() interface{} { return &utils.AddFileChunkEvent{} },
		"CommitFile":          func() interface{} { return &utils.CommitFileEvent{} },
		"FillChunkBlock":      func() interface{} { return &utils.FillChunkBlockEvent{} },
		"AddFileBlocks":       func() interface{} { return &utils.AddFileBlocksEvent{} },
		"AddPeerBlock":        func() interface{} { return &utils.AddPeerBlockEvent{} },
		"DeleteBucket":        func() interface{} { return &utils.DeleteBucketEvent{} },
		"DeletePeerBlock":     func() interface{} { return &utils.DeletePeerBlockEvent{} },
		"DeleteFile":          func() interface{} { return &utils.DeleteFileEvent{} },
		"Initialized":         func() interface{} { return &utils.InitializedEvent{} },
		"Upgraded":            func() interface{} { return &utils.UpgradedEvent{} },
		"EIP712DomainChanged": func() interface{} { return &utils.EIP712DomainChangedEvent{} },
	}

	for name, event := range contractABI.Events {
		if factory, ok := factories[name]; ok {
			eventRegistry[event.ID] = utils.EventMeta{
				Name:    name,
				Factory: factory,
			}
		}
	}
}

func decodeLog(log types.Log, eventName string, out interface{}) (*utils.DecodedEvent, error) {
	if err := utils.UnpackEvent(contractABI, out, eventName, log); err != nil {
		return nil, fmt.Errorf("failed to unpack event %s: %w", eventName, err)
	}

	decoded := &utils.DecodedEvent{
		EventName:       eventName,
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		BlockHash:       log.BlockHash,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Topics:          log.Topics,
		Data:            structToMap(eventName, out),
	}

	return decoded, nil
}

func DecodeAnyLog(log types.Log) (*utils.DecodedEvent, error) {
	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}

	topic := log.Topics[0]
	meta, ok := eventRegistry[topic]
	if !ok {
		return nil, fmt.Errorf("unknown event signature: %s", topic.Hex())
	}

	return decodeLog(log, meta.Name, meta.Factory())
}

func structToMap(eventName string, s interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	if s == nil {
		return result
	}

	val := reflect.Indirect(reflect.ValueOf(s))
	if !val.IsValid() || val.Kind() != reflect.Struct {
		return result
	}

	event, ok := contractABI.Events[eventName]
	if !ok {
		return result
	}

	abiInputs := make(map[string]abi.Argument)
	for _, arg := range event.Inputs {
		abiInputs[arg.Name] = arg
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		key := strings.Split(field.Tag.Get("json"), ",")[0]

		if hash, ok := fieldVal.Interface().(common.Hash); ok {
			result[key] = hash.Hex()
		} else {
			result[key] = fieldVal.Interface()
		}
	}
	return result
}
