package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertRepository_Create_Keyword(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	conds, _ := json.Marshal(model.KeywordConditions{
		Service:       "payment-api",
		Keyword:       "FATAL",
		WindowMinutes: 5,
	})

	rule, err := repo.Create(context.Background(), model.CreateAlertRequest{
		Name:       "test-keyword-rule",
		Type:       model.AlertTypeKeyword,
		Conditions: conds,
	})
	require.NoError(t, err)
	require.NotNil(t, rule)
	assert.Equal(t, "test-keyword-rule", rule.Name)
	assert.Equal(t, model.AlertTypeKeyword, rule.Type)
	assert.True(t, rule.Enabled)
	assert.NotNil(t, rule.ID)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM alert_rules WHERE id = $1", rule.ID)
	})
}

func TestAlertRepository_Create_Threshold(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	conds, _ := json.Marshal(model.ThresholdConditions{
		Service:       "payment-api",
		Level:         "error",
		Threshold:     10,
		WindowMinutes: 5,
	})

	rule, err := repo.Create(context.Background(), model.CreateAlertRequest{
		Name:       "test-threshold-rule",
		Type:       model.AlertTypeThreshold,
		Conditions: conds,
	})
	require.NoError(t, err)
	assert.Equal(t, model.AlertTypeThreshold, rule.Type)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM alert_rules WHERE id = $1", rule.ID)
	})
}

func TestAlertRepository_Create_Validation(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	t.Run("missing name", func(t *testing.T) {
		_, err := repo.Create(context.Background(), model.CreateAlertRequest{
			Type:       model.AlertTypeKeyword,
			Conditions: json.RawMessage(`{"keyword":"FATAL","window_minutes":5}`),
		})
		require.Error(t, err)
	})

	t.Run("missing keyword in keyword rule", func(t *testing.T) {
		_, err := repo.Create(context.Background(), model.CreateAlertRequest{
			Name:       "bad-rule",
			Type:       model.AlertTypeKeyword,
			Conditions: json.RawMessage(`{"window_minutes":5}`),
		})
		require.Error(t, err)
	})

	t.Run("threshold zero", func(t *testing.T) {
		_, err := repo.Create(context.Background(), model.CreateAlertRequest{
			Name:       "bad-threshold",
			Type:       model.AlertTypeThreshold,
			Conditions: json.RawMessage(`{"threshold":0,"window_minutes":5}`),
		})
		require.Error(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := repo.Create(context.Background(), model.CreateAlertRequest{
			Name:       "bad-type",
			Type:       "unknown",
			Conditions: json.RawMessage(`{}`),
		})
		require.Error(t, err)
	})
}

func TestAlertRepository_List(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	conds, _ := json.Marshal(model.KeywordConditions{Keyword: "ERROR", WindowMinutes: 5})
	rule, err := repo.Create(context.Background(), model.CreateAlertRequest{
		Name:       "test-list-rule",
		Type:       model.AlertTypeKeyword,
		Conditions: conds,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM alert_rules WHERE id = $1", rule.ID)
	})

	rules, err := repo.List(context.Background())
	require.NoError(t, err)

	found := false
	for _, r := range rules {
		if r.ID != nil && *r.ID == *rule.ID {
			found = true
		}
	}
	assert.True(t, found, "created rule should appear in list")
}

func TestAlertRepository_Delete(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	conds, _ := json.Marshal(model.KeywordConditions{Keyword: "ERROR", WindowMinutes: 5})
	rule, err := repo.Create(context.Background(), model.CreateAlertRequest{
		Name:       "test-delete-rule",
		Type:       model.AlertTypeKeyword,
		Conditions: conds,
	})
	require.NoError(t, err)

	deleted, err := repo.Delete(context.Background(), *rule.ID)
	require.NoError(t, err)
	assert.True(t, deleted)

	// Second delete should return false (not found).
	deleted, err = repo.Delete(context.Background(), *rule.ID)
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestAlertRepository_RecordEvent(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAlertRepository(pool)

	conds, _ := json.Marshal(model.KeywordConditions{Keyword: "FATAL", WindowMinutes: 5})
	rule, err := repo.Create(context.Background(), model.CreateAlertRequest{
		Name:       "test-event-rule",
		Type:       model.AlertTypeKeyword,
		Conditions: conds,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM alert_rules WHERE id = $1", rule.ID)
	})

	details, _ := json.Marshal(model.AlertEventDetails{
		RuleName:   rule.Name,
		RuleType:   string(rule.Type),
		Keyword:    "FATAL",
		WindowMins: 5,
	})

	ev, err := repo.RecordEvent(context.Background(), *rule.ID, 3, details)
	require.NoError(t, err)
	assert.NotNil(t, ev.ID)
	assert.Equal(t, 3, ev.MatchCount)

	// Verify it appears in ListEvents.
	events, err := repo.ListEvents(context.Background(), *rule.ID)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, 3, events[0].MatchCount)
}