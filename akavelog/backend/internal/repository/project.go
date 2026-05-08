package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	apikeys "github.com/akave-ai/akavelog/internal/model/api_keys"
	"github.com/akave-ai/akavelog/internal/model/projects"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const keyPrefix = "akal_" // easy to recognise in logs / env vars

// ProjectRepository handles persistence for projects and api_keys.
type ProjectRepository struct {
	pool *pgxpool.Pool
}

// NewProjectRepository creates a repository backed by the given pool.
func NewProjectRepository(pool *pgxpool.Pool) *ProjectRepository {
	return &ProjectRepository{pool: pool}
}

// ── Projects ──────────────────────────────────────────────────────────────────

// Create inserts a new project and generates one API key in a single transaction.
// The raw API key is returned to the caller exactly once — it is never stored again.
func (r *ProjectRepository) Create(ctx context.Context, req projects.CreateProjectRequest) (*projects.CreateProjectResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	rawKey, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generate api key: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Insert project.
	var proj projects.Project
	const insertProject = `
		INSERT INTO projects (name, owner_email)
		VALUES ($1, $2)
		RETURNING id, name, owner_email, created_at
	`
	err = tx.QueryRow(ctx, insertProject, req.Name, req.OwnerEmail).Scan(
		&proj.ID, &proj.Name, &proj.OwnerEmail, &proj.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	// Insert initial API key (key column = raw key; no hashing for simplicity).
	const insertKey = `
		INSERT INTO api_keys (key, project_id, name, active)
		VALUES ($1, $2, $3, TRUE)
	`
	if _, err := tx.Exec(ctx, insertKey, rawKey, proj.ID, "default"); err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &projects.CreateProjectResponse{Project: proj, APIKey: rawKey}, nil
}

// List returns all projects ordered by created_at DESC.
func (r *ProjectRepository) List(ctx context.Context) ([]projects.Project, error) {
	const q = `SELECT id, name, owner_email, created_at FROM projects ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var out []projects.Project
	for rows.Next() {
		var p projects.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.OwnerEmail, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projects rows: %w", err)
	}
	if out == nil {
		out = []projects.Project{}
	}
	return out, nil
}

// Get returns one project by ID.
func (r *ProjectRepository) Get(ctx context.Context, id uuid.UUID) (*projects.Project, error) {
	const q = `SELECT id, name, owner_email, created_at FROM projects WHERE id = $1`
	var p projects.Project
	err := r.pool.QueryRow(ctx, q, id).Scan(&p.ID, &p.Name, &p.OwnerEmail, &p.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &p, nil
}

// Delete removes a project (cascades to api_keys and alert_rules).
func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete project: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ── API Keys ──────────────────────────────────────────────────────────────────

// CreateAPIKey generates and persists a new API key for the given project.
func (r *ProjectRepository) CreateAPIKey(ctx context.Context, projectID uuid.UUID, name string) (*apikeys.APIKey, error) {
	rawKey, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if name == "" {
		name = "key-" + rawKey[len(keyPrefix):len(keyPrefix)+8]
	}

	const q = `
		INSERT INTO api_keys (key, project_id, name, active)
		VALUES ($1, $2, $3, TRUE)
		RETURNING key, project_id, name, active, created_at, last_used_at
	`
	var k apikeys.APIKey
	err = r.pool.QueryRow(ctx, q, rawKey, projectID, name).Scan(
		&k.Key, &k.ProjectID, &k.Name, &k.Active, &k.CreatedAt, &k.LastUsedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}
	return &k, nil
}

// ListAPIKeys returns all API keys for a project.
func (r *ProjectRepository) ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]apikeys.APIKey, error) {
	const q = `
		SELECT key, project_id, name, active, created_at, last_used_at
		FROM api_keys
		WHERE project_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, q, projectID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var out []apikeys.APIKey
	for rows.Next() {
		var k apikeys.APIKey
		if err := rows.Scan(&k.Key, &k.ProjectID, &k.Name, &k.Active, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("api keys rows: %w", err)
	}
	if out == nil {
		out = []apikeys.APIKey{}
	}
	return out, nil
}

// RevokeAPIKey sets active=false for the given key.
func (r *ProjectRepository) RevokeAPIKey(ctx context.Context, key string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET active = FALSE WHERE key = $1 AND active = TRUE`, key)
	if err != nil {
		return false, fmt.Errorf("revoke api key: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ResolveAPIKey looks up an active API key and returns its project_id.
// Updates last_used_at as a side-effect. Returns ("", nil) when not found.
func (r *ProjectRepository) ResolveAPIKey(ctx context.Context, key string) (uuid.UUID, error) {
	const q = `
		UPDATE api_keys
		SET    last_used_at = NOW()
		WHERE  key    = $1
		  AND  active = TRUE
		RETURNING project_id
	`
	var projectID uuid.UUID
	err := r.pool.QueryRow(ctx, q, key).Scan(&projectID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("resolve api key: %w", err)
	}
	return projectID, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// generateAPIKey creates a cryptographically-random key with the akal_ prefix.
func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return keyPrefix + hex.EncodeToString(b), nil
}

// TouchAPIKey is an alias used in tests.
func (r *ProjectRepository) TouchAPIKey(ctx context.Context, key string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = $1 WHERE key = $2`, time.Now().UTC(), key)
	return err
}