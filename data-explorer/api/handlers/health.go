package handlers

import (
	"net/http"

	"data-explorer/database"
)

// Health handles GET /health.
// It calls db.GetStats and returns the result as JSON, including a status field.
func Health(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := db.GetStats(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database unavailable")
			return
		}
		stats["status"] = "ok"
		writeJSON(w, http.StatusOK, stats)
	}
}
