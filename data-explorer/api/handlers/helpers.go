package handlers

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string, details ...string) {
	body := map[string]any{"error": msg}
	if len(details) > 0 {
		body["details"] = details
	}
	writeJSON(w, code, body)
}
