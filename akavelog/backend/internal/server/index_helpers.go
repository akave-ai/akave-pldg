package server

import (
	"time"

	logbatches "github.com/akave-ai/akavelog/internal/model/log_batches"
)

// logbatchesQueryParams builds a QueryParams for the repository from HTTP query values.
// Kept in its own file to make server.go easier to read.
func logbatchesQueryParams(tenant string, from, to time.Time) logbatches.QueryParams {
	return logbatches.QueryParams{
		Tenant:  tenant,
		TsStart: from,
		TsEnd:   to,
	}
}
