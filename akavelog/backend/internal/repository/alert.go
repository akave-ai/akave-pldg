package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akave-ai/akavelog/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertRepository handles persistence for alert_rules and alert_events.
type AlertRepository struct {
	pool *pgxpool.Pool
}

// NewAlertRepository creates a repository backed by the given connection pool.
func NewAlertRepository(pool *pgxpool.Pool) *AlertRepository {
	return &AlertRepository{pool: pool}
}

// Create inserts a new alert rule and returns the created row.
func (r *AlertRepository) Create(ctx context.Context, req model.CreateAlertRequest) (*model.AlertRule, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("alert rule name is required")
	}
	if req.Type != model.AlertTypeKeyword && req.Type != model.AlertTypeThreshold {
		return nil, fmt.Errorf("alert rule type must be 'keyword' or 'threshold'")
	}
	if len(req.Conditions) == 0 || string(req.Conditions) == "null" {
		return nil, fmt.Errorf("alert rule conditions are required")
	}

	// Validate conditions match the type.
	if err := validateConditions(req.Type, req.Conditions); err != nil {
		return nil, fmt.Errorf("invalid conditions: %w", err)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	actions := req.Actions
	if len(actions) == 0 {
		actions = json.RawMessage(`{}`)
	}

	const q = `
		INSERT INTO alert_rules (name, type, conditions, actions, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, project_id, name, type, conditions, actions, enabled, created_at, updated_at
	`

	var rule model.AlertRule
	err := r.pool.QueryRow(ctx, q,
		req.Name,
		string(req.Type),
		[]byte(req.Conditions),
		[]byte(actions),
		enabled,
	).Scan(
		&rule.ID,
		&rule.ProjectID,
		&rule.Name,
		&rule.Type,
		&rule.Conditions,
		&rule.Actions,
		&rule.Enabled,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create alert rule: %w", err)
	}
	return &rule, nil
}

// List returns all alert rules ordered by created_at DESC.
func (r *AlertRepository) List(ctx context.Context) ([]model.AlertRule, error) {
	const q = `
		SELECT id, project_id, name, type, conditions, actions, enabled, created_at, updated_at
		FROM alert_rules
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	var out []model.AlertRule
	for rows.Next() {
		var rule model.AlertRule
		if err := rows.Scan(
			&rule.ID, &rule.ProjectID, &rule.Name, &rule.Type,
			&rule.Conditions, &rule.Actions, &rule.Enabled,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alert rules rows: %w", err)
	}
	if out == nil {
		out = []model.AlertRule{}
	}
	return out, nil
}

// Get returns a single alert rule by ID.
func (r *AlertRepository) Get(ctx context.Context, id uuid.UUID) (*model.AlertRule, error) {
	const q = `
		SELECT id, project_id, name, type, conditions, actions, enabled, created_at, updated_at
		FROM alert_rules
		WHERE id = $1
	`
	var rule model.AlertRule
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&rule.ID, &rule.ProjectID, &rule.Name, &rule.Type,
		&rule.Conditions, &rule.Actions, &rule.Enabled,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get alert rule: %w", err)
	}
	return &rule, nil
}

// Delete removes an alert rule by ID (cascades to alert_events).
// Returns true if a row was deleted, false if not found.
func (r *AlertRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `DELETE FROM alert_rules WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return false, fmt.Errorf("delete alert rule: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ListEnabled returns all enabled alert rules for the background worker.
func (r *AlertRepository) ListEnabled(ctx context.Context) ([]model.AlertRule, error) {
	const q = `
		SELECT id, project_id, name, type, conditions, actions, enabled, created_at, updated_at
		FROM alert_rules
		WHERE enabled = TRUE
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list enabled alert rules: %w", err)
	}
	defer rows.Close()

	var out []model.AlertRule
	for rows.Next() {
		var rule model.AlertRule
		if err := rows.Scan(
			&rule.ID, &rule.ProjectID, &rule.Name, &rule.Type,
			&rule.Conditions, &rule.Actions, &rule.Enabled,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan enabled alert rule: %w", err)
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("enabled alert rules rows: %w", err)
	}
	return out, nil
}

// RecordEvent inserts an alert_events row when a rule fires.
func (r *AlertRepository) RecordEvent(ctx context.Context, ruleID uuid.UUID, matchCount int, details json.RawMessage) (*model.AlertEvent, error) {
	const q = `
		INSERT INTO alert_events (rule_id, match_count, details)
		VALUES ($1, $2, $3)
		RETURNING id, rule_id, triggered_at, match_count, details
	`
	var ev model.AlertEvent
	err := r.pool.QueryRow(ctx, q, ruleID, matchCount, []byte(details)).Scan(
		&ev.ID, &ev.RuleID, &ev.TriggeredAt, &ev.MatchCount, &ev.Details,
	)
	if err != nil {
		return nil, fmt.Errorf("record alert event: %w", err)
	}
	return &ev, nil
}

// ListEvents returns recent alert events for a rule (last 50).
func (r *AlertRepository) ListEvents(ctx context.Context, ruleID uuid.UUID) ([]model.AlertEvent, error) {
	const q = `
		SELECT id, rule_id, triggered_at, match_count, details
		FROM alert_events
		WHERE rule_id = $1
		ORDER BY triggered_at DESC
		LIMIT 50
	`
	rows, err := r.pool.Query(ctx, q, ruleID)
	if err != nil {
		return nil, fmt.Errorf("list alert events: %w", err)
	}
	defer rows.Close()

	var out []model.AlertEvent
	for rows.Next() {
		var ev model.AlertEvent
		if err := rows.Scan(&ev.ID, &ev.RuleID, &ev.TriggeredAt, &ev.MatchCount, &ev.Details); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alert events rows: %w", err)
	}
	if out == nil {
		out = []model.AlertEvent{}
	}
	return out, nil
}

// ── Validation helpers ────────────────────────────────────────────────────

func validateConditions(t model.AlertType, raw json.RawMessage) error {
	switch t {
	case model.AlertTypeKeyword:
		var c model.KeywordConditions
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		if c.Keyword == "" {
			return fmt.Errorf("keyword condition requires 'keyword' field")
		}
		if c.WindowMinutes < 0 {
			return fmt.Errorf("window_minutes must be non-negative")
		}
	case model.AlertTypeThreshold:
		var c model.ThresholdConditions
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		if c.Threshold <= 0 {
			return fmt.Errorf("threshold condition requires 'threshold' > 0")
		}
		if c.WindowMinutes < 0 {
			return fmt.Errorf("window_minutes must be non-negative")
		}
	}
	return nil
}