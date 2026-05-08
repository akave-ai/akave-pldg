package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// contextKey is a typed key for context values injected by this middleware.
type contextKey string

const (
	// ContextKeyProjectID is the context key for the resolved project UUID.
	ContextKeyProjectID contextKey = "project_id"
	// ContextKeyAPIKey is the context key for the raw API key string.
	ContextKeyAPIKey contextKey = "api_key"
)

// APIKeyResolver resolves a raw API key to its project UUID.
// Implemented by repository.ProjectRepository.ResolveAPIKey.
type APIKeyResolver interface {
	ResolveAPIKey(ctx context.Context, key string) (uuid.UUID, error)
}

// RequireAPIKey returns an Echo middleware that:
//  1. Reads the X-API-Key header.
//  2. Resolves it via the resolver (DB lookup + last_used_at touch).
//  3. Stores the project_id and api_key in the request context.
//  4. Returns 401 if the header is missing or the key is inactive/unknown.
//
// Use on routes that require authentication (ingest, query, alerts).
// The /projects endpoints themselves are intentionally left unauthenticated
// so operators can bootstrap the first project.
func RequireAPIKey(resolver APIKeyResolver) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rawKey := c.Request().Header.Get("X-API-Key")
			if rawKey == "" {
				return c.JSON(http.StatusUnauthorized, map[string]any{
					"error":   "unauthorized",
					"message": "X-API-Key header is required",
				})
			}

			projectID, err := resolver.ResolveAPIKey(c.Request().Context(), rawKey)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{
					"error":   "internal_error",
					"message": "failed to validate API key",
				})
			}
			if projectID == uuid.Nil {
				return c.JSON(http.StatusUnauthorized, map[string]any{
					"error":   "unauthorized",
					"message": "invalid or revoked API key",
				})
			}

			// Inject into both Echo context and stdlib context so downstream
			// handlers (and the ingester) can access the project_id.
			c.Set(string(ContextKeyProjectID), projectID)
			c.Set(string(ContextKeyAPIKey), rawKey)

			req := c.Request().WithContext(
				context.WithValue(
					context.WithValue(c.Request().Context(), ContextKeyProjectID, projectID),
					ContextKeyAPIKey, rawKey,
				),
			)
			c.SetRequest(req)

			return next(c)
		}
	}
}

// ProjectIDFromContext extracts the project UUID injected by RequireAPIKey.
// Returns uuid.Nil if not present (unauthenticated path).
func ProjectIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ContextKeyProjectID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// ProjectIDFromEcho extracts the project UUID from Echo's context store.
func ProjectIDFromEcho(c echo.Context) uuid.UUID {
	if v, ok := c.Get(string(ContextKeyProjectID)).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}