package models

import (
	"encoding/json"
	"time"
)

// ActionListItem is the compact shape returned in paginated list responses.
type ActionListItem struct {
	ID        int64     `json:"id"`
	BlockNum  int64     `json:"block_num"`
	BlockTime time.Time `json:"block_time"`
	TxHash    string    `json:"tx_hash"`
	Method    string    `json:"method"`
	Caller    string    `json:"caller"`
	Contract  string    `json:"contract"`
}

// ActionDetail is the full shape returned for a single-action lookup.
type ActionDetail struct {
	ActionListItem
	TxParams json.RawMessage `json:"tx_params"`
	Events   json.RawMessage `json:"events"`
	Value    string          `json:"value"` // wei as decimal string
}

// ActionsPage is the envelope for paginated list responses.
type ActionsPage struct {
	Data       []ActionListItem `json:"data"`
	Count      int              `json:"count"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
