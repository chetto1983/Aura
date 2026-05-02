package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrIssueNotFound is returned when a wiki_issues row doesn't exist.
var ErrIssueNotFound = errors.New("scheduler: wiki issue not found")

// ErrIssueAlreadyResolved is returned when Resolve targets an already-resolved
// issue. API layer maps to 409.
var ErrIssueAlreadyResolved = errors.New("scheduler: wiki issue already resolved")

// Issue is one row from wiki_issues.
type Issue struct {
	ID         int64
	Kind       string // "broken_link" | "orphan" | "missing_category"
	Severity   string // "high" | "medium" | "low"
	Slug       string // page that contains the issue
	BrokenLink string // broken slug target (for broken_link kind)
	Message    string
	Status     string // "open" | "resolved"
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

// IssuesStore provides CRUD over the wiki_issues table.
type IssuesStore struct {
	db *sql.DB
}

// NewIssuesStore wraps an existing *sql.DB. Migration must already be applied.
func NewIssuesStore(db *sql.DB) *IssuesStore {
	return &IssuesStore{db: db}
}

// Enqueue inserts an issue. Idempotent on (kind, slug, broken_link):
// if a row with the same key exists, this is a no-op.
func (s *IssuesStore) Enqueue(ctx context.Context, issue Issue) error {
	const q = `
		INSERT INTO wiki_issues (kind, severity, slug, broken_link, message, status)
		VALUES (?, ?, ?, ?, ?, 'open')
		ON CONFLICT(kind, slug, broken_link) DO NOTHING`
	_, err := s.db.ExecContext(ctx, q,
		issue.Kind, issue.Severity, issue.Slug, issue.BrokenLink, issue.Message)
	if err != nil {
		return fmt.Errorf("issues enqueue: %w", err)
	}
	return nil
}

// List returns issues filtered by status (empty = all), ordered by created_at DESC.
func (s *IssuesStore) List(ctx context.Context, status string) ([]Issue, error) {
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, kind, severity, slug, broken_link, message, status, created_at, resolved_at
			 FROM wiki_issues WHERE status = ? ORDER BY created_at DESC`, status)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, kind, severity, slug, broken_link, message, status, created_at, resolved_at
			 FROM wiki_issues ORDER BY created_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("issues list: %w", err)
	}
	defer rows.Close()

	var out []Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, issue)
	}
	return out, rows.Err()
}

// Get returns a single issue by ID, or ErrIssueNotFound.
func (s *IssuesStore) Get(ctx context.Context, id int64) (Issue, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, kind, severity, slug, broken_link, message, status, created_at, resolved_at
		 FROM wiki_issues WHERE id = ?`, id)
	issue, err := scanIssue(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Issue{}, ErrIssueNotFound
		}
		return Issue{}, fmt.Errorf("issues get: %w", err)
	}
	return issue, nil
}

// Resolve marks an issue as resolved. Returns ErrIssueNotFound if no such
// row exists, or ErrIssueAlreadyResolved if the row is not in status=open.
func (s *IssuesStore) Resolve(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE wiki_issues SET status = 'resolved', resolved_at = ? WHERE id = ? AND status = 'open'`,
		now, id)
	if err != nil {
		return fmt.Errorf("issues resolve: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("issues resolve rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	// UPDATE matched zero rows: distinguish not-found from already-resolved.
	var status string
	switch err := s.db.QueryRowContext(ctx,
		`SELECT status FROM wiki_issues WHERE id = ?`, id).Scan(&status); {
	case errors.Is(err, sql.ErrNoRows):
		return ErrIssueNotFound
	case err != nil:
		return fmt.Errorf("issues resolve lookup: %w", err)
	default:
		return ErrIssueAlreadyResolved
	}
}

type issueScanner interface {
	Scan(dest ...any) error
}

func scanIssue(r issueScanner) (Issue, error) {
	var issue Issue
	var createdAt string
	var resolvedAt sql.NullString
	if err := r.Scan(
		&issue.ID, &issue.Kind, &issue.Severity, &issue.Slug,
		&issue.BrokenLink, &issue.Message, &issue.Status,
		&createdAt, &resolvedAt,
	); err != nil {
		return Issue{}, err
	}
	ts, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return Issue{}, fmt.Errorf("parse created_at: %w", err)
		}
	}
	issue.CreatedAt = ts.UTC()
	if resolvedAt.Valid && resolvedAt.String != "" {
		rt, err := time.Parse(time.RFC3339, resolvedAt.String)
		if err == nil {
			rt = rt.UTC()
			issue.ResolvedAt = &rt
		}
	}
	return issue, nil
}
