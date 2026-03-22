package projects

import (
	"time"

	"github.com/google/uuid"
)

// Project is one tenant project. Logs are scoped to a project via its API keys.
type Project struct {
	ID         uuid.UUID `db:"id"         json:"id"`
	Name       string    `db:"name"       json:"name"`
	OwnerEmail string    `db:"owner_email" json:"owner_email,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// CreateProjectRequest is the body for POST /projects.
type CreateProjectRequest struct {
	Name       string `json:"name"`
	OwnerEmail string `json:"owner_email,omitempty"`
}

// CreateProjectResponse includes the created project and its initial API key.
type CreateProjectResponse struct {
	Project Project `json:"project"`
	APIKey  string  `json:"api_key"` // returned once only; not stored in plain text
}