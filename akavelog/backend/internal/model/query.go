package model

import "time"

// QueryRequest is the parsed body for POST /query.
// All filter fields are optional; omitted fields match everything.
type QueryRequest struct {
	// Time range. If TsStart is zero it defaults to 1 hour ago.
	// If TsEnd is zero it defaults to now.
	TsStart time.Time `json:"time_start"`
	TsEnd   time.Time `json:"time_end"`

	// Optional filters.
	Tenant  string   `json:"tenant"`  // defaults to "default"
	Service string   `json:"service"` // e.g. "payment-api"
	Levels  []string `json:"levels"`  // e.g. ["error", "warn"]
	Keyword string   `json:"keyword"` // case-insensitive substring match on line

	// Pagination.
	Limit int `json:"limit"` // max results; default 100, max 1000
}

// QueryResultEntry is one matching log line returned by the query engine.
type QueryResultEntry struct {
	TsNs        int64             `json:"ts_ns"`
	Timestamp   string            `json:"timestamp"` // RFC3339Nano
	Service     string            `json:"service"`
	Level       string            `json:"level"`
	Line        string            `json:"line"`
	Labels      map[string]string `json:"labels"`
	O3ObjectKey string            `json:"o3_object_key"`
}

// QueryResponse is the response body for POST /query.
type QueryResponse struct {
	Results   []QueryResultEntry `json:"results"`
	Count     int                `json:"count"`
	Truncated bool               `json:"truncated"` // true when limit was reached
}
