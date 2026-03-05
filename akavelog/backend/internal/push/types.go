package push

// PushRequest is the in-memory representation of a push (streams/values).
// After parse, each stream has entries with TsNs and Line.
type PushRequest struct {
	Streams []StreamRef `json:"streams"`
}

// StreamRef is one stream: labels + entries.
type StreamRef struct {
	Labels  map[string]string `json:"stream"`
	Entries []Entry            `json:"entries"`
}

// Entry is a single log line: timestamp (nanoseconds) and line text.
type Entry struct {
	TsNs int64  `json:"ts_ns"`
	Line string `json:"line"`
}
