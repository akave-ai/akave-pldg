package apikeys

import (
	"time"

	"github.com/google/uuid"
)

// APIKey is a credential that authorises log ingestion and queries for a project.
type APIKey struct {
	Key        string     `db:"key"         json:"key"`
	ProjectID  uuid.UUID  `db:"project_id"  json:"project_id"`
	Name       string     `db:"name"        json:"name"`
	Active     bool       `db:"active"      json:"active"`
	CreatedAt  time.Time  `db:"created_at"  json:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
}

// CreateAPIKeyRequest is the body for POST /projects/:id/api-keys.
type CreateAPIKeyRequest struct {
	Name string `json:"name,omitempty"`
}

// RevokeAPIKeyRequest is the body for DELETE /projects/:id/api-keys/:key.
type RevokeAPIKeyRequest struct{}