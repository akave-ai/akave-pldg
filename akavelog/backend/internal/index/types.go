package index

// Entry records one chunk reference for the Progress index (not TSDB).
// Used to discover which chunk keys to read for a given stream and time range.
type Entry struct {
	Tenant   string `json:"tenant"`
	StreamID string `json:"stream_id"`
	FromNs   int64  `json:"from_ns"`
	ToNs     int64  `json:"to_ns"`
	ChunkKey string `json:"chunk_key"`
}
