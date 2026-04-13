package handlers

import (
	"net/http"

	"data-explorer/database"
)

// Methods handles GET /methods — returns all distinct decoded method names.
func Methods(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		methods, err := db.GetDistinctMethods(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not fetch methods")
			return
		}
		if methods == nil {
			methods = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": methods})
	}
}
