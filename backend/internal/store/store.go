// Package store provides PostgreSQL-backed persistence using pgx. uuid columns are
// cast to text in queries so they scan cleanly into Go strings.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

// New opens a connection pool to the given database URL.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// ---- projects ----

const projectCols = `id::text, slug, name, github_owner, github_repo, github_project_id, created_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.GitHubOwner, &p.GitHubRepo, &p.GitHubProjectID, &p.CreatedAt)
	return p, err
}

func (s *Store) CreateProject(ctx context.Context, slug, name, owner, repo, projectNodeID string) (Project, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (slug, name, github_owner, github_repo, github_project_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+projectCols,
		slug, name, owner, repo, projectNodeID)
	return scanProject(row)
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (Project, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE slug = $1`, slug)
	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) GetProjectByID(ctx context.Context, id string) (Project, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE id = $1`, id)
	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+projectCols+` FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- users ----

const userCols = `auth_code, project_id::text, display_name, email, revoked_at, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.AuthCode, &u.ProjectID, &u.DisplayName, &u.Email, &u.RevokedAt, &u.CreatedAt)
	return u, err
}

func (s *Store) CreateUser(ctx context.Context, authCode, projectID, displayName string, email *string) (User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (auth_code, project_id, display_name, email)
		VALUES ($1, $2, $3, $4)
		RETURNING `+userCols,
		authCode, projectID, displayName, email)
	return scanUser(row)
}

// GetActiveUser looks up a non-revoked user by auth_code and returns the user with
// their project. Returns ErrNotFound for unknown or revoked codes.
func (s *Store) GetActiveUser(ctx context.Context, authCode string) (User, Project, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT u.auth_code, u.project_id::text, u.display_name, u.email, u.revoked_at, u.created_at,
		       p.id::text, p.slug, p.name, p.github_owner, p.github_repo, p.github_project_id, p.created_at
		FROM users u JOIN projects p ON p.id = u.project_id
		WHERE u.auth_code = $1 AND u.revoked_at IS NULL`, authCode)

	var u User
	var p Project
	err := row.Scan(
		&u.AuthCode, &u.ProjectID, &u.DisplayName, &u.Email, &u.RevokedAt, &u.CreatedAt,
		&p.ID, &p.Slug, &p.Name, &p.GitHubOwner, &p.GitHubRepo, &p.GitHubProjectID, &p.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, Project{}, ErrNotFound
	}
	if err != nil {
		return User{}, Project{}, err
	}
	return u, p, nil
}

// GetUserByCode returns a user regardless of revocation status (used for retries).
func (s *Store) GetUserByCode(ctx context.Context, authCode string) (User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE auth_code = $1`, authCode)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) RevokeUser(ctx context.Context, authCode string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET revoked_at = now() WHERE auth_code = $1 AND revoked_at IS NULL`, authCode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context, projectID string) ([]User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userCols+` FROM users WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---- submissions ----

const submissionCols = `id::text, auth_code, project_id::text, title, body, status,
	github_issue_number, github_issue_url, github_node_id, error, created_at`

func scanSubmission(row pgx.Row) (Submission, error) {
	var sub Submission
	err := row.Scan(&sub.ID, &sub.AuthCode, &sub.ProjectID, &sub.Title, &sub.Body, &sub.Status,
		&sub.GitHubIssueNumber, &sub.GitHubIssueURL, &sub.GitHubNodeID, &sub.Error, &sub.CreatedAt)
	return sub, err
}

// CreateSubmission inserts a pending submission and returns its id.
func (s *Store) CreateSubmission(ctx context.Context, authCode, projectID, title, body string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO submissions (auth_code, project_id, title, body, status)
		VALUES ($1, $2, $3, $4, 'pending')
		RETURNING id::text`,
		authCode, projectID, title, body).Scan(&id)
	return id, err
}

func (s *Store) MarkSubmissionCreated(ctx context.Context, id string, issueNumber int, issueURL, nodeID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE submissions
		SET status = 'created', github_issue_number = $2, github_issue_url = $3,
		    github_node_id = $4, error = NULL
		WHERE id = $1`,
		id, issueNumber, issueURL, nodeID)
	return err
}

func (s *Store) MarkSubmissionFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE submissions SET status = 'failed', error = $2 WHERE id = $1`, id, errMsg)
	return err
}

// ListUnpublished returns submissions still pending or failed, oldest first, for retry.
func (s *Store) ListUnpublished(ctx context.Context, limit int) ([]Submission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+submissionCols+`
		FROM submissions WHERE status <> 'created'
		ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Submission
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
