package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AlertType defines the kind of alert rule.
type AlertType string

const (
	AlertTypeKeyword   AlertType = "keyword"
	AlertTypeThreshold AlertType = "threshold"
)

// AlertRule is a user-defined rule evaluated by the background worker.
type AlertRule struct {
	ID         *uuid.UUID      `json:"id"`
	ProjectID  *uuid.UUID      `json:"project_id,omitempty"`
	Name       string          `json:"name"`
	Type       AlertType       `json:"type"`
	Conditions json.RawMessage `json:"conditions"`
	Actions    json.RawMessage `json:"actions"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// KeywordConditions is the condition payload for type=keyword rules.
// Triggers when keyword appears in any log line within the time window.
type KeywordConditions struct {
	Service       string `json:"service,omitempty"`  // optional; empty = all services
	Keyword       string `json:"keyword"`            // required; case-insensitive substring
	WindowMinutes int    `json:"window_minutes"`     // lookback window in minutes; default 5
}

// ThresholdConditions is the condition payload for type=threshold rules.
// Triggers when matching log count exceeds threshold within the time window.
type ThresholdConditions struct {
	Service       string `json:"service,omitempty"` // optional; empty = all services
	Level         string `json:"level,omitempty"`   // optional; empty = all levels
	Keyword       string `json:"keyword,omitempty"` // optional additional filter
	Threshold     int    `json:"threshold"`         // minimum count to trigger
	WindowMinutes int    `json:"window_minutes"`    // lookback window in minutes; default 5
}

// AlertActions defines what happens when an alert fires.
type AlertActions struct {
	WebhookURL string `json:"webhook_url,omitempty"` // optional HTTP POST target
}

// AlertEvent is a record of one alert rule firing.
type AlertEvent struct {
	ID          *uuid.UUID      `json:"id"`
	RuleID      *uuid.UUID      `json:"rule_id"`
	TriggeredAt time.Time       `json:"triggered_at"`
	MatchCount  int             `json:"match_count"`
	Details     json.RawMessage `json:"details"`
}

// CreateAlertRequest is the request body for POST /alerts.
type CreateAlertRequest struct {
	Name       string          `json:"name"`
	Type       AlertType       `json:"type"`
	Conditions json.RawMessage `json:"conditions"`
	Actions    json.RawMessage `json:"actions,omitempty"`
	Enabled    *bool           `json:"enabled,omitempty"` // defaults to true
}

// AlertEventDetails is stored in alert_events.details for human-readable context.
type AlertEventDetails struct {
	RuleName   string `json:"rule_name"`
	RuleType   string `json:"rule_type"`
	Service    string `json:"service,omitempty"`
	Keyword    string `json:"keyword,omitempty"`
	Level      string `json:"level,omitempty"`
	WindowMins int    `json:"window_minutes"`
}