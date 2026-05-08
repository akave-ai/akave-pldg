package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	akavemiddleware "github.com/akave-ai/akavelog/internal/middleware"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── stub resolver ─────────────────────────────────────────────────────────────

type stubResolver struct {
	m map[string]uuid.UUID // key → projectID
}

func (s *stubResolver) ResolveAPIKey(_ context.Context, key string) (uuid.UUID, error) {
	id, ok := s.m[key]
	if !ok {
		return uuid.Nil, nil
	}
	return id, nil
}

// ── helpers ────────────────────────────────────────────────────────────────────

func echoContext(method, path, apiKey string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestRequireAPIKey_MissingHeader(t *testing.T) {
	resolver := &stubResolver{m: map[string]uuid.UUID{}}
	mw := akavemiddleware.RequireAPIKey(resolver)

	called := false
	next := func(c echo.Context) error { called = true; return nil }

	c, rec := echoContext(http.MethodGet, "/query", "")
	err := mw(next)(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called)
}

func TestRequireAPIKey_InvalidKey(t *testing.T) {
	resolver := &stubResolver{m: map[string]uuid.UUID{}}
	mw := akavemiddleware.RequireAPIKey(resolver)

	called := false
	next := func(c echo.Context) error { called = true; return nil }

	c, rec := echoContext(http.MethodGet, "/query", "akal_badkey")
	err := mw(next)(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called)
}

func TestRequireAPIKey_ValidKey(t *testing.T) {
	projectID := uuid.New()
	resolver := &stubResolver{m: map[string]uuid.UUID{
		"akal_goodkey123": projectID,
	}}
	mw := akavemiddleware.RequireAPIKey(resolver)

	var gotProjectID uuid.UUID
	next := func(c echo.Context) error {
		gotProjectID = akavemiddleware.ProjectIDFromEcho(c)
		return nil
	}

	c, rec := echoContext(http.MethodPost, "/query", "akal_goodkey123")
	err := mw(next)(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, projectID, gotProjectID)
}

func TestRequireAPIKey_InjectsIntoStdlibContext(t *testing.T) {
	projectID := uuid.New()
	resolver := &stubResolver{m: map[string]uuid.UUID{
		"akal_ctxkey": projectID,
	}}
	mw := akavemiddleware.RequireAPIKey(resolver)

	var ctxProjectID uuid.UUID
	next := func(c echo.Context) error {
		ctxProjectID = akavemiddleware.ProjectIDFromContext(c.Request().Context())
		return nil
	}

	c, _ := echoContext(http.MethodPost, "/akavelog/api/v1/push", "akal_ctxkey")
	require.NoError(t, mw(next)(c))
	assert.Equal(t, projectID, ctxProjectID)
}

func TestProjectIDFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()
	id := akavemiddleware.ProjectIDFromContext(ctx)
	assert.Equal(t, uuid.Nil, id)
}

func TestProjectIDFromEcho_NotPresent(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	id := akavemiddleware.ProjectIDFromEcho(c)
	assert.Equal(t, uuid.Nil, id)
}