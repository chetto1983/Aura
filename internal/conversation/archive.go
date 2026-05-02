package conversation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrDuplicateTurn is returned by Append when a turn with the same
// (chat_id, turn_index) already exists.
var ErrDuplicateTurn = errors.New("conversation: duplicate (chat_id, turn_index)")

const migrationCreateConversationsTable = `
CREATE TABLE IF NOT EXISTS conversations (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id           INTEGER NOT NULL,
  user_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  role              TEXT    NOT NULL,
  content           TEXT    NOT NULL,
  tool_calls        TEXT,
  tool_call_id      TEXT,
  llm_calls         INTEGER NOT NULL DEFAULT 0,
  tool_calls_count  INTEGER NOT NULL DEFAULT 0,
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  tokens_in         INTEGER NOT NULL DEFAULT 0,
  tokens_out        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(chat_id, turn_index)
);
CREATE INDEX IF NOT EXISTS idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_conv_user ON conversations(user_id, created_at);
`

// Turn is a single archived message from a conversation.
type Turn struct {
	ID             int64
	ChatID         int64
	UserID         int64
	TurnIndex      int64
	Role           string // "user" | "assistant" | "tool"
	Content        string
	ToolCalls      string // JSON; empty for non-assistant roles
	ToolCallID     string // non-empty for role=tool
	LLMCalls       int
	ToolCallsCount int
	ElapsedMS      int64
	TokensIn       int
	TokensOut      int
	CreatedAt      time.Time
}

// ArchiveStore persists conversation turns in SQLite.
type ArchiveStore struct {
	db *sql.DB
}

// NewArchiveStore wraps an existing *sql.DB and applies the conversations
// migration. The DB is owned by the caller.
func NewArchiveStore(db *sql.DB) (*ArchiveStore, error) {
	if _, err := db.Exec(migrationCreateConversationsTable); err != nil {
		return nil, fmt.Errorf("conversation archive migrate: %w", err)
	}
	return &ArchiveStore{db: db}, nil
}

// Append inserts a single Turn. Returns ErrDuplicateTurn if a row with the
// same (chat_id, turn_index) already exists.
func (s *ArchiveStore) Append(ctx context.Context, t Turn) error {
	const q = `
		INSERT INTO conversations
			(chat_id, user_id, turn_index, role, content, tool_calls, tool_call_id,
			 llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	toolCalls := sql.NullString{String: t.ToolCalls, Valid: t.ToolCalls != ""}
	toolCallID := sql.NullString{String: t.ToolCallID, Valid: t.ToolCallID != ""}

	_, err := s.db.ExecContext(ctx, q,
		t.ChatID, t.UserID, t.TurnIndex, t.Role, t.Content,
		toolCalls, toolCallID,
		t.LLMCalls, t.ToolCallsCount, t.ElapsedMS, t.TokensIn, t.TokensOut,
	)
	if err != nil {
		if isDuplicateError(err) {
			return ErrDuplicateTurn
		}
		return fmt.Errorf("conversation append: %w", err)
	}
	return nil
}

// ListByChat returns up to limit turns for chatID, ordered newest-first.
func (s *ArchiveStore) ListByChat(ctx context.Context, chatID int64, limit int) ([]Turn, error) {
	const q = `
		SELECT id, chat_id, user_id, turn_index, role, content, tool_calls, tool_call_id,
		       llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at
		FROM conversations
		WHERE chat_id = ?
		ORDER BY turn_index DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("conversation list: %w", err)
	}
	defer rows.Close()

	out := []Turn{}
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns a single Turn by its primary key, or sql.ErrNoRows if absent.
func (s *ArchiveStore) Get(ctx context.Context, id int64) (Turn, error) {
	const q = `
		SELECT id, chat_id, user_id, turn_index, role, content, tool_calls, tool_call_id,
		       llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at
		FROM conversations
		WHERE id = ?`

	row := s.db.QueryRowContext(ctx, q, id)
	t, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Turn{}, sql.ErrNoRows
		}
		return Turn{}, fmt.Errorf("conversation get: %w", err)
	}
	return t, nil
}

type turnScanner interface {
	Scan(dest ...any) error
}

func scanTurn(r turnScanner) (Turn, error) {
	var (
		t          Turn
		toolCalls  sql.NullString
		toolCallID sql.NullString
		createdAt  string
	)
	if err := r.Scan(
		&t.ID, &t.ChatID, &t.UserID, &t.TurnIndex, &t.Role, &t.Content,
		&toolCalls, &toolCallID,
		&t.LLMCalls, &t.ToolCallsCount, &t.ElapsedMS, &t.TokensIn, &t.TokensOut,
		&createdAt,
	); err != nil {
		return Turn{}, err
	}
	if toolCalls.Valid {
		t.ToolCalls = toolCalls.String
	}
	if toolCallID.Valid {
		t.ToolCallID = toolCallID.String
	}
	ts, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		// Try RFC3339 fallback.
		ts, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return Turn{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
	}
	t.CreatedAt = ts.UTC()
	return t, nil
}

// isDuplicateError reports whether err is a SQLite unique-constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// modernc.org/sqlite wraps the SQLite error text.
	return contains(msg, "UNIQUE constraint failed") || contains(msg, "SQLITE_CONSTRAINT")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexInString(s, sub) >= 0)
}

func indexInString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
