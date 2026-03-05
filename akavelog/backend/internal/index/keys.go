package index

import (
	"path"
	"time"

	"github.com/google/uuid"
)

// KeyForIndexBatch returns the object key for an index batch file in O3.
// Convention: index/<tenant>/<date>/<batchID>.ndjson
// date = 2006-01-02 (UTC), one batch per write (no append; O3 has no append).
func KeyForIndexBatch(tenant string, date time.Time, batchID string) string {
	if tenant == "" {
		tenant = "default"
	}
	if batchID == "" {
		batchID = uuid.New().String()
	}
	dateStr := date.UTC().Format("2006-01-02")
	return path.Join("index", tenant, dateStr, batchID+".ndjson")
}

// PrefixForTenantDate returns the prefix to list index batches for a tenant on a date.
// Use with storage.ListObjects to get all index files for that day.
func PrefixForTenantDate(tenant string, date time.Time) string {
	if tenant == "" {
		tenant = "default"
	}
	return path.Join("index", tenant, date.UTC().Format("2006-01-02")) + "/"
}

// PrefixForTenant returns the prefix for all index data for a tenant.
func PrefixForTenant(tenant string) string {
	if tenant == "" {
		tenant = "default"
	}
	return path.Join("index", tenant) + "/"
}
