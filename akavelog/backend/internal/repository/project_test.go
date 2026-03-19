package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akave-ai/akavelog/internal/model/projects"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ────────────────────────────────────────────────────────────────

func cleanupProject(t *testing.T, repo *repository.ProjectRepository, id uuid.UUID) {
	t.Helper()
	_, _ = repo.Delete(context.Background(), id)
}

// ── Project CRUD ──────────────────────────────────────────────────────────────

func TestProjectRepository_Create(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{
		Name:       "test-project-create",
		OwnerEmail: "dev@example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	assert.Equal(t, "test-project-create", resp.Project.Name)
	assert.Equal(t, "dev@example.com", resp.Project.OwnerEmail)
	assert.NotEqual(t, uuid.Nil, resp.Project.ID)
	assert.NotEmpty(t, resp.APIKey)
	assert.True(t, strings.HasPrefix(resp.APIKey, "akal_"), "key should have akal_ prefix")
}

func TestProjectRepository_Create_RequiresName(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	_, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: ""})
	require.Error(t, err)
}

func TestProjectRepository_Create_UniqueNames(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-unique-name"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	// Creating another project with the same name should fail (UNIQUE constraint).
	_, err = repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-unique-name"})
	require.Error(t, err)
}

func TestProjectRepository_List(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-list-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	list, err := repo.List(context.Background())
	require.NoError(t, err)

	found := false
	for _, p := range list {
		if p.ID == resp.Project.ID {
			found = true
		}
	}
	assert.True(t, found, "created project must appear in list")
}

func TestProjectRepository_Get(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-get-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	p, err := repo.Get(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "test-get-proj", p.Name)

	// Non-existent UUID should return nil, nil.
	p2, err := repo.Get(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Nil(t, p2)
}

func TestProjectRepository_Delete(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-delete-proj"})
	require.NoError(t, err)

	deleted, err := repo.Delete(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	assert.True(t, deleted)

	// Second delete → not found.
	deleted, err = repo.Delete(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	assert.False(t, deleted)
}

// ── API Key management ────────────────────────────────────────────────────────

func TestProjectRepository_CreateAPIKey(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-apikey-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	key, err := repo.CreateAPIKey(context.Background(), resp.Project.ID, "ci-key")
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.True(t, strings.HasPrefix(key.Key, "akal_"))
	assert.Equal(t, "ci-key", key.Name)
	assert.True(t, key.Active)
	assert.Equal(t, resp.Project.ID, key.ProjectID)
}

func TestProjectRepository_ListAPIKeys(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-listkeys-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	// The Create call already generated a "default" key.
	keys, err := repo.ListAPIKeys(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	assert.Len(t, keys, 1, "one default key should exist")

	// Add a second key.
	_, err = repo.CreateAPIKey(context.Background(), resp.Project.ID, "extra")
	require.NoError(t, err)

	keys, err = repo.ListAPIKeys(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestProjectRepository_RevokeAPIKey(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-revoke-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	rawKey := resp.APIKey

	revoked, err := repo.RevokeAPIKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.True(t, revoked)

	// Revoking an already-revoked key → false.
	revoked, err = repo.RevokeAPIKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.False(t, revoked)
}

func TestProjectRepository_ResolveAPIKey(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-resolve-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	rawKey := resp.APIKey

	// Valid key → returns project ID.
	projectID, err := repo.ResolveAPIKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.Equal(t, resp.Project.ID, projectID)

	// Unknown key → uuid.Nil.
	unknown, err := repo.ResolveAPIKey(context.Background(), "akal_notreal000")
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, unknown)

	// Revoke → resolve returns Nil.
	_, err = repo.RevokeAPIKey(context.Background(), rawKey)
	require.NoError(t, err)

	revoked, err := repo.ResolveAPIKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, revoked)
}

func TestProjectRepository_ResolveAPIKey_UpdatesLastUsed(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-lastused-proj"})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupProject(t, repo, resp.Project.ID) })

	before := time.Now().UTC().Add(-time.Second)

	_, err = repo.ResolveAPIKey(context.Background(), resp.APIKey)
	require.NoError(t, err)

	// Check last_used_at was updated.
	keys, err := repo.ListAPIKeys(context.Background(), resp.Project.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.NotNil(t, keys[0].LastUsedAt)
	assert.True(t, keys[0].LastUsedAt.After(before))
}

// ── Cascade delete ────────────────────────────────────────────────────────────

func TestProjectRepository_DeleteCascadesKeys(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewProjectRepository(pool)

	resp, err := repo.Create(context.Background(), projects.CreateProjectRequest{Name: "test-cascade-proj"})
	require.NoError(t, err)

	rawKey := resp.APIKey
	projectID := resp.Project.ID

	// Delete project.
	_, err = repo.Delete(context.Background(), projectID)
	require.NoError(t, err)

	// Key should no longer resolve.
	pid, err := repo.ResolveAPIKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, pid)
}