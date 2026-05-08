package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type InputState string

const (
	InputStateRunning InputState = "RUNNING"
	InputStateStopped InputState = "STOPPED"
	InputStatePaused  InputState = "PAUSED"
)

type Input struct {
	ID            uuid.UUID       `db:"id"`
	Type          string          `db:"type"`
	Title         string          `db:"title"`
	Configuration json.RawMessage `db:"configuration"`
	Global        bool            `db:"global"`
	NodeID        string          `db:"node_id"`
	CreatorUserID string          `db:"creator_user_id"`
	CreatedAt     time.Time       `db:"created_at"`
	DesiredState  InputState      `db:"desired_state"`
}
