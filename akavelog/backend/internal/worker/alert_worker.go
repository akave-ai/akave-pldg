package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/akave-ai/akavelog/internal/model"
	"github.com/google/uuid"
)

const (
	defaultEvalInterval  = 60 * time.Second
	defaultWindowMinutes = 5
)

// RuleStore is the subset of AlertRepository the worker needs.
type RuleStore interface {
	ListEnabled(ctx context.Context) ([]model.AlertRule, error)
	RecordEvent(ctx context.Context, ruleID uuid.UUID, matchCount int, details json.RawMessage) (*model.AlertEvent, error)
}

// LogCounter counts matching log entries in a time window.
// Implemented by a thin wrapper around the query engine.
type LogCounter interface {
	CountLogs(ctx context.Context, service, level, keyword string, from, to time.Time) (int, error)
}

// AlertWorker is a background goroutine that evaluates alert rules on a schedule.
type AlertWorker struct {
	rules    RuleStore
	counter  LogCounter
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// New creates an AlertWorker.
// interval defaults to 60s if zero.
func New(rules RuleStore, counter LogCounter, interval time.Duration) *AlertWorker {
	if interval <= 0 {
		interval = defaultEvalInterval
	}
	return &AlertWorker{
		rules:    rules,
		counter:  counter,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the evaluation loop in the background.
func (w *AlertWorker) Start(ctx context.Context) {
	go w.run(ctx)
}

// Stop signals the worker to exit and waits for it to finish.
func (w *AlertWorker) Stop() {
	close(w.stop)
	<-w.done
}

func (w *AlertWorker) run(ctx context.Context) {
	defer close(w.done)
	log.Printf("[alert-worker] started (interval=%v)", w.interval)

	// Run once immediately on start, then on ticker.
	w.evalAll(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.evalAll(ctx)
		}
	}
}

// evalAll fetches all enabled rules and evaluates each one.
func (w *AlertWorker) evalAll(ctx context.Context) {
	rules, err := w.rules.ListEnabled(ctx)
	if err != nil {
		log.Printf("[alert-worker] list enabled rules: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	log.Printf("[alert-worker] evaluating %d rule(s)", len(rules))
	for _, rule := range rules {
		w.evalRule(ctx, rule)
	}
}

// evalRule evaluates one rule and records an event if it fires.
func (w *AlertWorker) evalRule(ctx context.Context, rule model.AlertRule) {
	switch rule.Type {
	case model.AlertTypeKeyword:
		w.evalKeyword(ctx, rule)
	case model.AlertTypeThreshold:
		w.evalThreshold(ctx, rule)
	default:
		log.Printf("[alert-worker] unknown rule type %q for rule %v", rule.Type, rule.ID)
	}
}

func (w *AlertWorker) evalKeyword(ctx context.Context, rule model.AlertRule) {
	var cond model.KeywordConditions
	if err := json.Unmarshal(rule.Conditions, &cond); err != nil {
		log.Printf("[alert-worker] parse keyword conditions for rule %v: %v", rule.ID, err)
		return
	}
	if cond.WindowMinutes <= 0 {
		cond.WindowMinutes = defaultWindowMinutes
	}

	to := time.Now().UTC()
	from := to.Add(-time.Duration(cond.WindowMinutes) * time.Minute)

	count, err := w.counter.CountLogs(ctx, cond.Service, "", cond.Keyword, from, to)
	if err != nil {
		log.Printf("[alert-worker] count logs for rule %v: %v", rule.ID, err)
		return
	}

	if count == 0 {
		return // no match — rule did not fire
	}

	log.Printf("[alert-worker] keyword rule %q fired: %d matches for keyword=%q service=%q",
		rule.Name, count, cond.Keyword, cond.Service)

	w.recordAndNotify(ctx, rule, count, model.AlertEventDetails{
		RuleName:   rule.Name,
		RuleType:   string(rule.Type),
		Service:    cond.Service,
		Keyword:    cond.Keyword,
		WindowMins: cond.WindowMinutes,
	})
}

func (w *AlertWorker) evalThreshold(ctx context.Context, rule model.AlertRule) {
	var cond model.ThresholdConditions
	if err := json.Unmarshal(rule.Conditions, &cond); err != nil {
		log.Printf("[alert-worker] parse threshold conditions for rule %v: %v", rule.ID, err)
		return
	}
	if cond.WindowMinutes <= 0 {
		cond.WindowMinutes = defaultWindowMinutes
	}
	if cond.Threshold <= 0 {
		log.Printf("[alert-worker] rule %v has threshold <= 0, skipping", rule.ID)
		return
	}

	to := time.Now().UTC()
	from := to.Add(-time.Duration(cond.WindowMinutes) * time.Minute)

	count, err := w.counter.CountLogs(ctx, cond.Service, cond.Level, cond.Keyword, from, to)
	if err != nil {
		log.Printf("[alert-worker] count logs for rule %v: %v", rule.ID, err)
		return
	}

	if count < cond.Threshold {
		return // threshold not reached — rule did not fire
	}

	log.Printf("[alert-worker] threshold rule %q fired: %d matches (threshold=%d) service=%q level=%q",
		rule.Name, count, cond.Threshold, cond.Service, cond.Level)

	w.recordAndNotify(ctx, rule, count, model.AlertEventDetails{
		RuleName:   rule.Name,
		RuleType:   string(rule.Type),
		Service:    cond.Service,
		Level:      cond.Level,
		Keyword:    cond.Keyword,
		WindowMins: cond.WindowMinutes,
	})
}

// recordAndNotify saves an alert_event and optionally fires a webhook.
func (w *AlertWorker) recordAndNotify(ctx context.Context, rule model.AlertRule, count int, details model.AlertEventDetails) {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		log.Printf("[alert-worker] marshal details for rule %v: %v", rule.ID, err)
		return
	}

	ev, err := w.rules.RecordEvent(ctx, *rule.ID, count, detailsJSON)
	if err != nil {
		log.Printf("[alert-worker] record event for rule %v: %v", rule.ID, err)
		return
	}
	log.Printf("[alert-worker] recorded event %v for rule %q (match_count=%d)", ev.ID, rule.Name, count)

	// Fire webhook if configured (non-fatal on failure).
	var actions model.AlertActions
	if len(rule.Actions) > 0 {
		_ = json.Unmarshal(rule.Actions, &actions)
	}
	if actions.WebhookURL != "" {
		go fireWebhook(actions.WebhookURL, rule, count, details)
	}
}

// fireWebhook sends a JSON POST to the webhook URL.
// Runs in its own goroutine — failure is logged but never propagated.
func fireWebhook(url string, rule model.AlertRule, count int, details model.AlertEventDetails) {
	payload := map[string]any{
		"rule_id":     fmt.Sprintf("%v", rule.ID),
		"rule_name":   rule.Name,
		"rule_type":   rule.Type,
		"match_count": count,
		"details":     details,
		"fired_at":    time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[alert-worker] webhook marshal: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[alert-worker] webhook request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "akavelog-alert-worker/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[alert-worker] webhook POST to %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("[alert-worker] webhook %s returned %d", url, resp.StatusCode)
		return
	}
	log.Printf("[alert-worker] webhook fired → %s (%d)", url, resp.StatusCode)
}

// ── LogCounter adapter ────────────────────────────────────────────────────

// QueryEngineCounter wraps the query engine's metadata lookup to count
// matching log_batches entries. This is a fast SQL-only count that does NOT
// fetch O3 objects — it counts batches whose metadata matches the filters.
// For keyword matching it falls back to a full engine query to get exact counts.
type QueryEngineCounter struct {
	repo BatchCounter
}

// BatchCounter is the interface for counting log batches by metadata.
type BatchCounter interface {
	CountByFilter(ctx context.Context, service, level, keyword string, from, to time.Time) (int, error)
}

// NewQueryEngineCounter creates a LogCounter backed by the given repository.
func NewQueryEngineCounter(repo BatchCounter) *QueryEngineCounter {
	return &QueryEngineCounter{repo: repo}
}

// CountLogs returns the number of log entries matching the given filters.
func (c *QueryEngineCounter) CountLogs(ctx context.Context, service, level, keyword string, from, to time.Time) (int, error) {
	return c.repo.CountByFilter(ctx, service, level, keyword, from, to)
}

// keep strings import used
var _ = strings.ToLower