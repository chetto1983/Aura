// Package agentnote provides per-conversation scratchpad storage for the
// agent's working memory. Notes are scoped to a single conversation and must
// be garbage-collected when the conversation ends.
package agentnote

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Store manages the agent_notes table. Each conversation has at most one row;
// Get/Set/Append/Clear provide atomic read-modify-write semantics.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by the provided database connection.
// The caller is responsible for ensuring that migrations have been applied.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get returns the current note content for conversationID.
// If no row exists, it returns ("", false, nil).
func (s *Store) Get(ctx context.Context, conversationID string) (content string, exists bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT content FROM agent_notes WHERE conversation_id = ?`, conversationID,
	).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("agentnote: get %q: %w", conversationID, err)
	}
	return content, true, nil
}

// Set replaces the note content for conversationID, creating the row if absent.
func (s *Store) Set(ctx context.Context, conversationID, content string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_notes (conversation_id, content, updated_at)
         VALUES (?, ?, unixepoch())
         ON CONFLICT(conversation_id) DO UPDATE
           SET content = excluded.content, updated_at = excluded.updated_at`,
		conversationID, content,
	)
	if err != nil {
		return fmt.Errorf("agentnote: set %q: %w", conversationID, err)
	}
	return nil
}

// Append adds line to the note for conversationID under a BEGIN IMMEDIATE
// transaction so the read-modify-write is atomic. If the note is empty or
// absent, content is set to line with no leading newline.
func (s *Store) Append(ctx context.Context, conversationID, line string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("agentnote: append %q acquire conn: %w", conversationID, err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("agentnote: append %q begin: %w", conversationID, err)
	}

	var current string
	scanErr := conn.QueryRowContext(ctx,
		`SELECT content FROM agent_notes WHERE conversation_id = ?`, conversationID,
	).Scan(&current)
	if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		conn.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck
		return fmt.Errorf("agentnote: append %q read: %w", conversationID, scanErr)
	}

	var newContent string
	if current == "" {
		newContent = line
	} else {
		newContent = current + "\n" + line
	}

	_, execErr := conn.ExecContext(ctx,
		`INSERT INTO agent_notes (conversation_id, content, updated_at)
         VALUES (?, ?, unixepoch())
         ON CONFLICT(conversation_id) DO UPDATE
           SET content = excluded.content, updated_at = excluded.updated_at`,
		conversationID, newContent,
	)
	if execErr != nil {
		conn.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck
		return fmt.Errorf("agentnote: append %q write: %w", conversationID, execErr)
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("agentnote: append %q commit: %w", conversationID, err)
	}
	return nil
}

// Clear deletes the note row for conversationID. It is not an error if the
// row does not exist.
func (s *Store) Clear(ctx context.Context, conversationID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_notes WHERE conversation_id = ?`, conversationID,
	)
	if err != nil {
		return fmt.Errorf("agentnote: clear %q: %w", conversationID, err)
	}
	return nil
}
