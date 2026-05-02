package conversation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrDuplicateTurn is returned by Append when a turn with the same
// (chat_id, turn_index) already exists.
var ErrDuplicateTurn = errors.New("conversation: duplicate (chat_id, turn_index)")

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

// NewArchiveStore wraps an existing *sql.DB. The conversations table
// migration is owned by scheduler.Store (single source of truth for the
// shared DB schema); callers must open a scheduler.Store on the same DB
// before constructing an ArchiveStore. Returns an error only if a future
// per-store migration fails — currently never errors.
func NewArchiveStore(db *sql.DB) (*ArchiveStore, error) {
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

// TurnAppender is the write side of ArchiveStore — satisfied by *ArchiveStore
// and by mock implementations in tests.
type TurnAppender interface {
	Append(ctx context.Context, t Turn) error
}

// BufferedAppender wraps a TurnAppender with a buffered channel and a single
// drain goroutine so hot conversation paths are non-blocking. Turns that
// arrive when the buffer is full are dropped and logged.
type BufferedAppender struct {
	store  TurnAppender
	ch     chan Turn
	logger *slog.Logger
	wg     sync.WaitGroup
}

// NewBufferedAppender starts the drain goroutine. bufSize should be 100 for
// production; tests may use smaller values.
func NewBufferedAppender(store TurnAppender, bufSize int) *BufferedAppender {
	a := &BufferedAppender{
		store:  store,
		ch:     make(chan Turn, bufSize),
		logger: slog.Default(),
	}
	a.wg.Add(1)
	go a.drain()
	return a
}

// Append enqueues a Turn non-blocking. If the buffer is full the turn is
// dropped and a warning is logged (archive_dropped_total).
func (a *BufferedAppender) Append(_ context.Context, t Turn) error {
	select {
	case a.ch <- t:
	default:
		a.logger.Warn("archive_dropped_total: buffer full, turn dropped",
			"chat_id", t.ChatID, "turn_index", t.TurnIndex)
	}
	return nil
}

// Close signals the drain goroutine to flush remaining turns and waits for it
// to finish. ctx is reserved for future timeout support.
func (a *BufferedAppender) Close(_ context.Context) error {
	close(a.ch)
	a.wg.Wait()
	return nil
}

func (a *BufferedAppender) drain() {
	defer a.wg.Done()
	for t := range a.ch {
		if err := a.store.Append(context.Background(), t); err != nil {
			if !errors.Is(err, ErrDuplicateTurn) {
				a.logger.Warn("archive drain: append failed",
					"chat_id", t.ChatID, "turn_index", t.TurnIndex, "error", err)
			}
		}
	}
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
