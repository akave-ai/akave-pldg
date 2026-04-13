package pagination

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Encode turns a (blockNum, id) pair into a url-safe opaque cursor string.
func Encode(blockNum, id int64) string {
	raw := fmt.Sprintf("%d:%d", blockNum, id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// Decode parses a cursor string produced by Encode.
func Decode(cursor string) (blockNum, id int64, err error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("malformed cursor")
	}
	blockNum, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("malformed cursor block_num: %w", err)
	}
	id, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("malformed cursor id: %w", err)
	}
	return blockNum, id, nil
}
