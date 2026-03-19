package handler

import (
	"context"
	"net/http"

	apikeys "github.com/akave-ai/akavelog/internal/model/api_keys"
	"github.com/akave-ai/akavelog/internal/model/projects"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// ProjectStore is the interface used by ProjectHandler.
type ProjectStore interface {
	Create(ctx context.Context, req projects.CreateProjectRequest) (*projects.CreateProjectResponse, error)
	List(ctx context.Context) ([]projects.Project, error)
	Get(ctx context.Context, id uuid.UUID) (*projects.Project, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	CreateAPIKey(ctx context.Context, projectID uuid.UUID, name string) (*apikeys.APIKey, error)
	ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]apikeys.APIKey, error)
	RevokeAPIKey(ctx context.Context, key string) (bool, error)
}

// ProjectHandler handles project and API-key CRUD endpoints.
type ProjectHandler struct {
	store ProjectStore
}

// NewProjectHandler creates a handler backed by the given store.
func NewProjectHandler(store ProjectStore) *ProjectHandler {
	return &ProjectHandler{store: store}
}

// ── Projects ──────────────────────────────────────────────────────────────────

// Create handles POST /projects.
//
// Request body: {"name":"my-project","owner_email":"dev@example.com"}
// Response: {project: {...}, api_key: "akal_..."}  (api_key shown once only)
func (h *ProjectHandler) Create(c echo.Context) error {
	var req projects.CreateProjectRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body", err.Error())
	}
	if req.Name == "" {
		return response.BadRequest(c, "validation failed", "name is required")
	}

	resp, err := h.store.Create(c.Request().Context(), req)
	if err != nil {
		return response.BadRequest(c, "create project failed", err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"project": resp.Project,
		"api_key": resp.APIKey,
		"note":    "Save your API key — it will not be shown again.",
	})
}

// List handles GET /projects.
func (h *ProjectHandler) List(c echo.Context) error {
	list, err := h.store.List(c.Request().Context())
	if err != nil {
		return response.InternalError(c, "list projects failed", err.Error())
	}
	return response.OK(c, map[string]any{"projects": list}, "")
}

// Get handles GET /projects/:id.
func (h *ProjectHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}
	p, err := h.store.Get(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c, "get project failed", err.Error())
	}
	if p == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "project not found"})
	}
	return response.OK(c, map[string]any{"project": p}, "")
}

// Delete handles DELETE /projects/:id.
func (h *ProjectHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}
	deleted, err := h.store.Delete(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c, "delete project failed", err.Error())
	}
	if !deleted {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "project not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

// ── API Keys ──────────────────────────────────────────────────────────────────

// CreateAPIKey handles POST /projects/:id/api-keys.
func (h *ProjectHandler) CreateAPIKey(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}

	var req struct {
		Name string `json:"name"`
	}
	_ = c.Bind(&req) // optional body

	key, err := h.store.CreateAPIKey(c.Request().Context(), id, req.Name)
	if err != nil {
		return response.InternalError(c, "create api key failed", err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"api_key": key,
		"note":    "Save your API key — it will not be shown again.",
	})
}

// ListAPIKeys handles GET /projects/:id/api-keys.
func (h *ProjectHandler) ListAPIKeys(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}
	keys, err := h.store.ListAPIKeys(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c, "list api keys failed", err.Error())
	}
	return response.OK(c, map[string]any{"api_keys": keys}, "")
}

// RevokeAPIKey handles DELETE /projects/:id/api-keys/:key.
func (h *ProjectHandler) RevokeAPIKey(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return response.BadRequest(c, "invalid key", "key param is required")
	}
	revoked, err := h.store.RevokeAPIKey(c.Request().Context(), key)
	if err != nil {
		return response.InternalError(c, "revoke api key failed", err.Error())
	}
	if !revoked {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "api key not found or already revoked"})
	}
	return c.NoContent(http.StatusNoContent)
}