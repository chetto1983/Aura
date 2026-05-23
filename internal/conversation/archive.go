package conversation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/storage/freshness"
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

// TurnAppender is the write side of the conversation archive.
type TurnAppender interface {
	Append(ctx context.Context, t Turn) error
}

// ClosingTurnAppender is the non-blocking archive writer lifecycle used by
// Telegram shutdown.
type ClosingTurnAppender interface {
	TurnAppender
	Close(ctx context.Context) error
}

// ChatTurnReader is the narrow read side for one chat.
type ChatTurnReader interface {
	ListByChat(ctx context.Context, chatID int64, limit int) ([]Turn, error)
}

// TurnReader is the read side for archive search and dashboard lists.
type TurnReader interface {
	ChatTurnReader
	ListAll(ctx context.Context, limit int) ([]Turn, error)
}

// TurnDetailReader reads one archived turn by primary key.
type TurnDetailReader interface {
	Get(ctx context.Context, id int64) (Turn, error)
}

// TurnIndexReader allocates durable per-chat turn indexes for append paths.
type TurnIndexReader interface {
	MaxTurnIndex(ctx context.Context, chatID int64) (int64, error)
}

// ArchiveStatsReader is the dashboard retention statistics surface.
type ArchiveStatsReader interface {
	Stats(ctx context.Context) (ArchiveStats, error)
}

// ArchiveDeleter is the dashboard retention mutation surface.
type ArchiveDeleter interface {
	DeleteByChat(ctx context.Context, chatID int64) (int64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteAll(ctx context.Context) (int64, error)
}

// ArchiveRepository is the full conversation archive boundary implemented by
// ArchiveStore.
type ArchiveRepository interface {
	TurnAppender
	TurnReader
	TurnDetailReader
	TurnIndexReader
	ArchiveStatsReader
	ArchiveDeleter
}

// ArchiveStore persists conversation turns in SQLite.
type ArchiveStore struct {
	db *sql.DB
}

// NewArchiveStore wraps an existing *sql.DB. The conversations table
// migration is owned by cron.Store (single source of truth for the
// shared DB schema); callers must open a cron.Store on the same DB
// before constructing an ArchiveStore. Returns an error only if a future
// per-store migration fails — currently never errors.
func NewArchiveStore(db *sql.DB) (*ArchiveStore, error) {
	return &ArchiveStore{db: db}, nil
}

// Append inserts a single Turn. Returns ErrDuplicateTurn if a row with the
// same (chat_id, turn_index) already exists. Empty assistant messages (no
// content, no tool_calls) are silently skipped — they are semantically empty
// and would poison subsequent provider API calls if replayed.
func (s *ArchiveStore) Append(ctx context.Context, t Turn) error {
	if t.Role == "assistant" && strings.TrimSpace(t.Content) == "" && t.ToolCalls == "" {
		slog.Default().Warn("archive: skip_empty_assistant_turn",
			"chat_id", t.ChatID,
			"turn_index", t.TurnIndex,
		)
		return nil
	}

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

// ListAll returns turns across all chats, newest first. When limit is positive
// it returns the most recent `limit` turns; when limit is zero or negative it
// returns all turns. Full rebuild paths use the unbounded form so compact
// projections do not silently drop older archive rows.
func (s *ArchiveStore) ListAll(ctx context.Context, limit int) ([]Turn, error) {
	q := `
		SELECT id, chat_id, user_id, turn_index, role, content, tool_calls, tool_call_id,
		       llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at
		FROM conversations
		ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		q += `
		LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("conversation list all: %w", err)
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

// MaxTurnIndex returns the highest turn_index stored for chatID, or -1 if
// no rows exist. Used by the bot to allocate monotonic turn indexes
// independently of in-memory conversation state (which the context-limit
// enforcer can trim).
func (s *ArchiveStore) MaxTurnIndex(ctx context.Context, chatID int64) (int64, error) {
	var maxIdx sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(turn_index) FROM conversations WHERE chat_id = ?`, chatID).
		Scan(&maxIdx)
	if err != nil {
		return -1, fmt.Errorf("conversation max turn_index: %w", err)
	}
	if !maxIdx.Valid {
		return -1, nil
	}
	return maxIdx.Int64, nil
}

// DeleteByChat removes every turn for chatID. Returns rows affected.
// Used by the dashboard's "wipe this chat's history" button.
func (s *ArchiveStore) DeleteByChat(ctx context.Context, chatID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE chat_id = ?`, chatID)
	if err != nil {
		return 0, fmt.Errorf("conversation delete by chat: %w", err)
	}
	return res.RowsAffected()
}

// DeleteOlderThan removes turns whose created_at is strictly older than
// the cutoff. Used by the dashboard's retention controls and by an
// optional auto-purge job. Returns rows affected.
func (s *ArchiveStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	// SQLite stores created_at via DEFAULT CURRENT_TIMESTAMP which is UTC
	// "YYYY-MM-DD HH:MM:SS". Compare against the same format.
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM conversations WHERE created_at < ?`,
		cutoff.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, fmt.Errorf("conversation delete older than: %w", err)
	}
	return res.RowsAffected()
}

// DeleteAll empties the conversations table. Used by the dashboard's
// "wipe everything" button — confirmed via UI, not exposed to the LLM.
func (s *ArchiveStore) DeleteAll(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversations`)
	if err != nil {
		return 0, fmt.Errorf("conversation delete all: %w", err)
	}
	return res.RowsAffected()
}

// Stats returns row count + DB size estimate for the dashboard.
type ArchiveStats struct {
	TotalRows     int64
	OldestAt      time.Time // zero when TotalRows == 0
	NewestAt      time.Time
	DistinctChats int64
}

// Stats summarizes the conversations table for the dashboard's retention
// controls. Cheap — three indexed aggregates.
func (s *ArchiveStore) Stats(ctx context.Context) (ArchiveStats, error) {
	var stats ArchiveStats
	var oldest, newest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			MIN(created_at),
			MAX(created_at),
			(SELECT COUNT(DISTINCT chat_id) FROM conversations)
		FROM conversations
	`).Scan(&stats.TotalRows, &oldest, &newest, &stats.DistinctChats)
	if err != nil {
		return ArchiveStats{}, fmt.Errorf("conversation stats: %w", err)
	}
	if oldest.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", oldest.String); err == nil {
			stats.OldestAt = t.UTC()
		}
	}
	if newest.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", newest.String); err == nil {
			stats.NewestAt = t.UTC()
		}
	}
	return stats, nil
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

// BufferedAppender wraps a TurnAppender with a buffered channel and a single
// drain goroutine so hot conversation paths are non-blocking. Turns that
// arrive when the buffer is full are dropped and logged.
type BufferedAppender struct {
	store          TurnAppender
	ch             chan Turn
	logger         *slog.Logger
	wg             sync.WaitGroup
	freshnessStore *freshness.Store
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
// dropped and an error is logged (archive_dropped_total).
func (a *BufferedAppender) Append(_ context.Context, t Turn) error {
	select {
	case a.ch <- t:
	default:
		a.logger.Error("archive_dropped_total: buffer full, turn dropped",
			"chat_id", t.ChatID, "turn_index", t.TurnIndex, "role", t.Role)
	}
	return nil
}

// SetFreshnessStore wires an optional freshness.Store so the drain goroutine
// bumps compact_memory_archive pending_count after every successful append.
// Nil-safe: passing nil disables the hook without side effects.
func (a *BufferedAppender) SetFreshnessStore(fs *freshness.Store) {
	a.freshnessStore = fs
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
				a.logger.Error("archive drain: append failed",
					"chat_id", t.ChatID, "turn_index", t.TurnIndex, "role", t.Role, "error", err)
			}
		} else if a.freshnessStore != nil {
			if bErr := a.freshnessStore.BumpPending(context.Background(), "compact_memory_archive", 1); bErr != nil {
				a.logger.Warn("archive drain: freshness bump failed", "error", bErr)
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
