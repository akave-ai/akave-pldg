package utils

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func UnpackEvent(contractABI abi.ABI, out interface{}, name string, vLog types.Log) error {
	// Unpack indexed and non-indexed event fields
	event := contractABI.Events[name]
	if len(vLog.Data) > 0 {
		if err := contractABI.UnpackIntoInterface(out, name, vLog.Data); err != nil {
			return err
		}
	}

	// Handle indexed parameters separately
	var indexed abi.Arguments
	for _, arg := range event.Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}

	// topic 0 is the event signature, so we skip it and start from topic 1
	if err := abi.ParseTopics(out, indexed, vLog.Topics[1:]); err != nil {
		return err
	}

	return nil
}

func HashToString(h common.Hash) string {
	return string(bytes.TrimRight(h[:], "\x00"))
}

// Formatting/Sanitizing functions

// SanitizeEvent creates a sanitized copy of the event with cleaned data.
func SanitizeEvent(ev *DecodedEvent) *DecodedEvent {
	if ev == nil {
		return nil
	}

	sanitized := &DecodedEvent{
		EventName:       ev.EventName,
		ContractAddress: ev.ContractAddress,
		BlockNumber:     ev.BlockNumber,
		BlockHash:       ev.BlockHash,
		TxHash:          ev.TxHash,
		LogIndex:        ev.LogIndex,
		Topics:          ev.Topics,
		Data:            sanitizeDataMap(ev.Data),
	}

	return sanitized
}

// sanitizeDataMap recursively sanitizes a map[string]interface{} to fix Unicode issues.
func sanitizeDataMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	sanitized := make(map[string]interface{})
	for k, v := range data {
		sanitized[k] = sanitizeValue(v)
	}
	return sanitized
}

// sanitizeValue recursively sanitizes values to fix Unicode escape sequences.
func sanitizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return SanitizeString(val)
	case []byte:
		// Convert []byte to hex string to avoid control chars and encoding issues
		return fmt.Sprintf("0x%x", val)
	case map[string]interface{}:
		return sanitizeDataMap(val)
	case []interface{}:
		sanitized := make([]interface{}, len(val))
		for i, item := range val {
			sanitized[i] = sanitizeValue(item)
		}
		return sanitized
	default:
		return v
	}
}

// SanitizeString fixes invalid Unicode escape sequences and removes control chars.
func SanitizeString(s string) string {
	// Ensure valid UTF-8
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}

	// Remove control characters (U+0000 to U+001F) - PostgreSQL rejects them
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if r >= 0 && r <= 0x1F {
			buf.WriteRune(' ') // Replace control chars with space
		} else {
			buf.WriteRune(r)
		}
	}
	s = buf.String()

	if !strings.Contains(s, `\u`) {
		return s
	}

	// Fix invalid \u sequences
	buf.Reset()
	buf.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == 'u' {
			if i+6 <= len(s) {
				hexPart := s[i+2 : i+6]
				valid := true
				for _, c := range hexPart {
					if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
						valid = false
						break
					}
				}
				if valid {
					buf.WriteString(s[i : i+6])
					i += 6
					continue
				}
			}
			buf.WriteString(`\\u`)
			i += 2
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}
	return buf.String()
}

// SanitizeJSONString fixes invalid Unicode escape sequences in a JSON string.
// PostgreSQL rejects \u0000-\u001F (control chars like null, tab) - replace with space.
func SanitizeJSONString(jsonStr string) string {
	var buf strings.Builder
	buf.Grow(len(jsonStr))

	i := 0
	for i < len(jsonStr) {
		if jsonStr[i] == '\\' && i+1 < len(jsonStr) && jsonStr[i+1] == 'u' {
			if i+6 <= len(jsonStr) {
				hexPart := jsonStr[i+2 : i+6]
				valid := true
				for _, c := range hexPart {
					if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
						valid = false
						break
					}
				}
				if valid {
					// Parse hex to check if it's a control character (U+0000 to U+001F)
					var code int
					_, err := fmt.Sscanf(hexPart, "%x", &code)
					if err != nil {
						log.Printf("Failed to scan hex part: %v", err)
						continue
					}
					if code <= 0x001F {
						// PostgreSQL rejects control chars - replace with space
						buf.WriteString(`\u0020`)
					} else {
						buf.WriteString(jsonStr[i : i+6])
					}
					i += 6
					continue
				}
			}
			buf.WriteString(`\\u`)
			i += 2
		} else {
			buf.WriteByte(jsonStr[i])
			i++
		}
	}

	return buf.String()
}
