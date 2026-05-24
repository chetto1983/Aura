package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
)

const compactionFocusPreviewMaxRunes = 160

// CompactionEvent is the durable debug record for one successful context
// compaction. It stores bounded metadata only: no prompt, transcript, or raw
// tool output payloads.
type CompactionEvent struct {
	ID               int64
	RunID            string
	ChatID           int64
	TurnIndex        int64
	Iteration        int
	MessagesBefore   int
	MessagesAfter    int
	TokensBefore     int
	TokensAfter      int
	ThresholdTokens  int
	CumulativeTokens int
	FocusPreview     string
	ElapsedMS        int64
	CreatedAt        time.Time
}

// CompactionRecorder is the write-side surface used by AutoCompactEngine.
type CompactionRecorder interface {
	RecordCompaction(ctx context.Context, event CompactionEvent) error
}

// CompactionReader is the dashboard read-side surface for per-turn compactions.
type CompactionReader interface {
	ListCompactionsForTurn(ctx context.Context, chatID, turnIndex int64) ([]CompactionEvent, error)
}

// CompactionRepository is implemented by ArchiveStore.
type CompactionRepository interface {
	CompactionRecorder
	CompactionReader
}

// RecordCompaction appends one compaction event. The event is intentionally not
// required to match a conversations row at write time because the Telegram
// archive path is buffered; the API joins by chat_id + turn_index when read.
func (s *ArchiveStore) RecordCompaction(ctx context.Context, event CompactionEvent) error {
	if s == nil || s.db == nil {
		return errors.New("conversation compaction recorder unavailable")
	}
	event = normalizeCompactionEvent(event)
	const q = `
		INSERT INTO conversation_compactions
			(run_id, chat_id, turn_index, iteration, messages_before, messages_after,
			 tokens_before, tokens_after, threshold_tokens, cumulative_tokens,
			 focus_preview, elapsed_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		event.RunID,
		event.ChatID,
		event.TurnIndex,
		event.Iteration,
		event.MessagesBefore,
		event.MessagesAfter,
		event.TokensBefore,
		event.TokensAfter,
		event.ThresholdTokens,
		event.CumulativeTokens,
		event.FocusPreview,
		event.ElapsedMS,
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("conversation record compaction: %w", err)
	}
	return nil
}

// ListCompactionsForTurn returns compactions for the archived conversation
// turn, oldest-first so the dashboard can show the sequence plainly.
func (s *ArchiveStore) ListCompactionsForTurn(ctx context.Context, chatID, turnIndex int64) ([]CompactionEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("conversation compaction reader unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, chat_id, turn_index, iteration, messages_before, messages_after,
		       tokens_before, tokens_after, threshold_tokens, cumulative_tokens,
		       focus_preview, elapsed_ms, created_at
		FROM conversation_compactions
		WHERE chat_id = ? AND turn_index = ?
		ORDER BY created_at ASC, id ASC`, chatID, turnIndex)
	if err != nil {
		return nil, fmt.Errorf("conversation list compactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []CompactionEvent{}
	for rows.Next() {
		event, scanErr := scanCompactionEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func normalizeCompactionEvent(event CompactionEvent) CompactionEvent {
	event.RunID = strings.TrimSpace(event.RunID)
	event.FocusPreview = RedactCompactionFocusPreview(event.FocusPreview)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Iteration = max(event.Iteration, 0)
	event.MessagesBefore = max(event.MessagesBefore, 0)
	event.MessagesAfter = max(event.MessagesAfter, 0)
	event.TokensBefore = max(event.TokensBefore, 0)
	event.TokensAfter = max(event.TokensAfter, 0)
	event.ThresholdTokens = max(event.ThresholdTokens, 0)
	event.CumulativeTokens = max(event.CumulativeTokens, 0)
	event.ElapsedMS = max(event.ElapsedMS, 0)
	return event
}

// RedactCompactionFocusPreview collapses focus text into a small, log-safe
// preview and drops obvious secret-bearing strings.
func RedactCompactionFocusPreview(focus string) string {
	focus = strings.Join(strings.Fields(focus), " ")
	if focus == "" {
		return ""
	}
	lower := strings.ToLower(focus)
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"api_key",
		"api key",
		"apikey",
		"access_token",
		"refresh_token",
		"password:",
		"password=",
		"secret:",
		"secret=",
		"sk-",
	} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	return truncateRunes(focus, compactionFocusPreviewMaxRunes)
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

type compactionScanner interface {
	Scan(dest ...any) error
}

func scanCompactionEvent(scanner compactionScanner) (CompactionEvent, error) {
	var (
		event     CompactionEvent
		createdAt string
	)
	if err := scanner.Scan(
		&event.ID,
		&event.RunID,
		&event.ChatID,
		&event.TurnIndex,
		&event.Iteration,
		&event.MessagesBefore,
		&event.MessagesAfter,
		&event.TokensBefore,
		&event.TokensAfter,
		&event.ThresholdTokens,
		&event.CumulativeTokens,
		&event.FocusPreview,
		&event.ElapsedMS,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CompactionEvent{}, sql.ErrNoRows
		}
		return CompactionEvent{}, fmt.Errorf("conversation scan compaction: %w", err)
	}
	ts, err := parseArchiveTimestamp(createdAt)
	if err != nil {
		return CompactionEvent{}, err
	}
	event.CreatedAt = ts
	return event, nil
}

func parseArchiveTimestamp(raw string) (time.Time, error) {
	if ts, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return ts.UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("parse archive timestamp %q", raw)
}

func estimateCompactionTokens(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += 4
		total += contentLengthForBudget(message.Content) / charsPerToken
		for _, call := range message.ToolCalls {
			total += toolCallBudgetChars(call) / charsPerToken
		}
	}
	return max(total, 0)
}

func toolCallBudgetChars(call llm.ToolCall) int {
	total := len(call.ID) + len(call.Name)
	if len(call.Arguments) > 0 {
		if raw, err := json.Marshal(call.Arguments); err == nil {
			total += len(raw)
		}
	}
	return total
}
