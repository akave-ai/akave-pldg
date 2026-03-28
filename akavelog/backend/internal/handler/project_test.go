package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akave-ai/akavelog/internal/handler"
	apikeys "github.com/akave-ai/akavelog/internal/model/api_keys"
	"github.com/akave-ai/akavelog/internal/model/projects"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── stub store ────────────────────────────────────────────────────────────────

type stubProjectStore struct {
	projects map[uuid.UUID]projects.Project
	keys     map[uuid.UUID][]apikeys.APIKey
}

func newStubStore() *stubProjectStore {
	return &stubProjectStore{
		projects: make(map[uuid.UUID]projects.Project),
		keys:     make(map[uuid.UUID][]apikeys.APIKey),
	}
}

func (s *stubProjectStore) Create(_ context.Context, req projects.CreateProjectRequest) (*projects.CreateProjectResponse, error) {
	id := uuid.New()
	p := projects.Project{ID: id, Name: req.Name, OwnerEmail: req.OwnerEmail}
	s.projects[id] = p
	return &projects.CreateProjectResponse{Project: p, APIKey: "akal_testkey123"}, nil
}

func (s *stubProjectStore) List(_ context.Context) ([]projects.Project, error) {
	out := make([]projects.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	return out, nil
}

func (s *stubProjectStore) Get(_ context.Context, id uuid.UUID) (*projects.Project, error) {
	if p, ok := s.projects[id]; ok {
		return &p, nil
	}
	return nil, nil
}

func (s *stubProjectStore) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	if _, ok := s.projects[id]; !ok {
		return false, nil
	}
	delete(s.projects, id)
	return true, nil
}

func (s *stubProjectStore) CreateAPIKey(_ context.Context, projectID uuid.UUID, name string) (*apikeys.APIKey, error) {
	k := apikeys.APIKey{Key: "akal_newkey456", ProjectID: projectID, Name: name, Active: true}
	s.keys[projectID] = append(s.keys[projectID], k)
	return &k, nil
}

func (s *stubProjectStore) ListAPIKeys(_ context.Context, projectID uuid.UUID) ([]apikeys.APIKey, error) {
	return s.keys[projectID], nil
}

func (s *stubProjectStore) RevokeAPIKey(_ context.Context, key string) (bool, error) {
	for pid, ks := range s.keys {
		for i, k := range ks {
			if k.Key == key {
				s.keys[pid][i].Active = false
				return true, nil
			}
		}
	}
	return false, nil
}

// ── test helpers ──────────────────────────────────────────────────────────────

func newEcho() *echo.Echo { e := echo.New(); return e }

func jsonRequest(method, path string, body any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return req
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestProjectHandler_Create(t *testing.T) {
	store := newStubStore()
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := jsonRequest(http.MethodPost, "/projects", map[string]string{
		"name": "acme", "owner_email": "ops@acme.com",
	})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.Create(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["api_key"])
	assert.NotNil(t, resp["project"])
}

func TestProjectHandler_Create_MissingName(t *testing.T) {
	store := newStubStore()
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := jsonRequest(http.MethodPost, "/projects", map[string]string{})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.Create(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestProjectHandler_List(t *testing.T) {
	store := newStubStore()
	_, _ = store.Create(context.Background(), projects.CreateProjectRequest{Name: "p1"})
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.List(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Len(t, data["projects"], 1)
}

func TestProjectHandler_Get_NotFound(t *testing.T) {
	store := newStubStore()
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(uuid.New().String())

	require.NoError(t, h.Get(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProjectHandler_Delete(t *testing.T) {
	store := newStubStore()
	resp, _ := store.Create(context.Background(), projects.CreateProjectRequest{Name: "del-me"})
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(resp.Project.ID.String())

	require.NoError(t, h.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestProjectHandler_CreateAPIKey(t *testing.T) {
	store := newStubStore()
	proj, _ := store.Create(context.Background(), projects.CreateProjectRequest{Name: "key-proj"})
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := jsonRequest(http.MethodPost, "/", map[string]string{"name": "ci"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(proj.Project.ID.String())

	require.NoError(t, h.CreateAPIKey(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotNil(t, body["api_key"])
}

func TestProjectHandler_RevokeAPIKey_NotFound(t *testing.T) {
	store := newStubStore()
	h := handler.NewProjectHandler(store)
	e := newEcho()

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "key")
	c.SetParamValues(uuid.New().String(), "akal_noexist")

	require.NoError(t, h.RevokeAPIKey(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}