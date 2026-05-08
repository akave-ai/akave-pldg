package model

// RawRequestData holds full HTTP request details for raw ingest logs.
type RawRequestData struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// LogEntry is the validated structure for an ingested log.
// Ingest payloads should be JSON with these fields.
type LogEntry struct {
	Timestamp  string            `json:"timestamp"`             // ISO8601 or Unix ms
	Service    string            `json:"service"`               // required
	Level      string            `json:"level"`                 // e.g. debug, info, warn, error
	Message    string            `json:"message"`               // required
	Tags       map[string]string `json:"tags,omitempty"`        // optional key-value
	ProjectID  string            `json:"project_id,omitempty"`  // optional; for multi-tenant
	RawRequest *RawRequestData   `json:"raw_request,omitempty"` // full HTTP request when ingested as raw
}
