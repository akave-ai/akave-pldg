package push

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ParsePushRequest parses JSON body: streams[].stream = labels, streams[].values = [ [ts_ns, line], ... ].
// Returns PushRequest with normalized entries (TsNs, Line). Invalid or empty lines are skipped.
func ParsePushRequest(body []byte) (*PushRequest, error) {
	var raw struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]interface{}   `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	req := &PushRequest{Streams: make([]StreamRef, 0, len(raw.Streams))}
	for _, s := range raw.Streams {
		labels := s.Stream
		if labels == nil {
			labels = make(map[string]string)
		}
		entries := make([]Entry, 0, len(s.Values))
		for _, v := range s.Values {
			if len(v) < 2 {
				continue
			}
			tsNs, ok := parseTsNs(v[0])
			if !ok {
				continue
			}
			line, _ := v[1].(string)
			entries = append(entries, Entry{TsNs: tsNs, Line: line})
		}
		req.Streams = append(req.Streams, StreamRef{Labels: labels, Entries: entries})
	}
	return req, nil
}

func parseTsNs(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	default:
		return 0, false
	}
}
