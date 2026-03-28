package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// AlertStore is the interface the handler uses for alert persistence.
type AlertStore interface {
	Create(ctx context.Context, req model.CreateAlertRequest) (*model.AlertRule, error)
	List(ctx context.Context) ([]model.AlertRule, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	ListEvents(ctx context.Context, ruleID uuid.UUID) ([]model.AlertEvent, error)
}

// AlertHandler handles alert rule CRUD endpoints.
type AlertHandler struct {
	store AlertStore
}

// NewAlertHandler creates a handler backed by the given store.
func NewAlertHandler(store AlertStore) *AlertHandler {
	return &AlertHandler{store: store}
}

// Create handles POST /alerts.
//
// Request body: model.CreateAlertRequest
//
// Examples:
//
//	Keyword rule:
//	{
//	  "name": "Fatal errors in worker",
//	  "type": "keyword",
//	  "conditions": {"service":"worker","keyword":"FATAL","window_minutes":5}
//	}
//
//	Threshold rule:
//	{
//	  "name": "High error rate in payment-api",
//	  "type": "threshold",
//	  "conditions": {"service":"payment-api","level":"error","threshold":10,"window_minutes":5}
//	}
func (h *AlertHandler) Create(c echo.Context) error {
	var req model.CreateAlertRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body", err.Error())
	}
	if req.Name == "" {
		return response.BadRequest(c, "validation failed", "name is required")
	}
	if req.Type == "" {
		return response.BadRequest(c, "validation failed", "type is required (keyword or threshold)")
	}
	if len(req.Conditions) == 0 {
		return response.BadRequest(c, "validation failed", "conditions are required")
	}

	rule, err := h.store.Create(c.Request().Context(), req)
	if err != nil {
		return response.BadRequest(c, "create alert rule failed", err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"alert": rule,
	})
}

// List handles GET /alerts.
// Returns all alert rules ordered by created_at DESC.
func (h *AlertHandler) List(c echo.Context) error {
	rules, err := h.store.List(c.Request().Context())
	if err != nil {
		return response.InternalError(c, "list alert rules failed", err.Error())
	}
	return response.OK(c, map[string]any{"alerts": rules}, "")
}

// Delete handles DELETE /alerts/:id.
// Returns 204 on success, 404 if not found.
func (h *AlertHandler) Delete(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}

	deleted, err := h.store.Delete(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c, "delete alert rule failed", err.Error())
	}
	if !deleted {
		return c.JSON(http.StatusNotFound, map[string]any{
			"error":   "not found",
			"message": "alert rule not found",
		})
	}
	return c.NoContent(http.StatusNoContent)
}

// ListEvents handles GET /alerts/:id/events.
// Returns the last 50 alert events for the given rule.
func (h *AlertHandler) ListEvents(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.BadRequest(c, "invalid id", "id must be a valid UUID")
	}

	events, err := h.store.ListEvents(c.Request().Context(), id)
	if err != nil {
		return response.InternalError(c, "list alert events failed", err.Error())
	}
	return response.OK(c, map[string]any{"events": events}, "")
}

// ensure AlertRepository satisfies AlertStore at compile time (checked in server.go via assignment)
var _ = json.Marshal // keep json import used