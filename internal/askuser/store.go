// Package askuser is the per-domain Store over aura.paused_states (PRD 1.5). It
// copies the canonical Store pattern proved in internal/identity (D-A4-01):
// Store{pool,q} over the generated sqlc surface, SQLSTATE-based error
// classification via errors.As + pgErr.Code (never message matching), sentinel
// errors, pgtype conversion at the boundary, and db.WithTx for atomic multi-row
// writes.
//
// Scope is the pause-PERSISTENCE half (SPEC Req#3): FIFO ListPending with a total
// order + crash recovery + Resume/Batch + Loop.Stop auto-resolve. The Runner
// (04-05) owns resume ORCHESTRATION and is the sole writer beyond this Store's
// Insert. This package NEVER imports internal/agent/tools (D-A1-04) — it takes
// plain fields; the Event carries the pause payload, not a tools type. There is
// no internal timeout / expiry state (SPEC Req#4): the only resolution paths are
// Resume, ResumeBatch, and AutoResolveForConversation.
package askuser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Action is the MCP three-action resolution model (AM-02 / D-A3-01). resumed_answer
// is stored as the JSON {action, content}: accept injects content as a RoleTool,
// decline tells the model the user declined, cancel aborts the turn.
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// autoTerminatedContent is the resumed_answer content written when a conversation
// ends with open pendings (SPEC Req#11). It is an accept-shaped marker so a
// resumed loop sees a benign answer rather than a missing one.
const autoTerminatedContent = "<auto-terminated: conversation ended>"

// Sentinel errors so callers classify failures without string matching.
// ErrPauseNotFound is an unknown/already-resolved token; ErrInvalidAnswer is a
// malformed resolution action.
var (
	ErrPauseNotFound = errors.New("paused state not found or already resumed")
	ErrInvalidAnswer = errors.New("invalid resume answer")
)

// Store wraps a pgx pool and the generated Queries — the canonical shape from
// internal/identity. Non-tx reads/writes use s.q; multi-row atomic writes
// (MarkResumedBatch, AutoResolveForConversation) wrap db.WithTx.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Pending is the domain projection of a pending aura.paused_states row — plain Go
// types at the package boundary instead of the sqlc pgtype wrappers.
type Pending struct {
	Token          string
	ConversationID string
	Kind           string
	Question       string
	Options        json.RawMessage
	Priority       int
	ToolCallID     string
}

// ResumeAnswer is the AM-02 resolution payload persisted as resumed_answer jsonb.
type ResumeAnswer struct {
	Action  string `json:"action"`
	Content string `json:"content"`
}

// InsertParams carries the plain fields for one new pause. NEVER a tools type
// (D-A1-04): the caller (the Runner, observing the pause Event) supplies these.
type InsertParams struct {
	Token          string
	ConversationID string
	Kind           string
	Question       string
	Options        json.RawMessage
	Priority       int
	ToolCallID     string
	ResumeContext  json.RawMessage
}

// Insert persists one pending pause. Options/ResumeContext are jsonb (nil → SQL
// NULL). proxied_* stay NULL for direct calls (D-A1-08; Phase 9 populates).
func (s *Store) Insert(ctx context.Context, p InsertParams) error {
	token, err := parseUUID("token", p.Token)
	if err != nil {
		return fmt.Errorf("insert paused state: %w", err)
	}
	convID, err := parseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return fmt.Errorf("insert paused state: %w", err)
	}
	arg := sqlc.InsertPausedStateParams{
		Token:          token,
		ConversationID: convID,
		Kind:           p.Kind,
		Question:       p.Question,
		Options:        p.Options,
		Priority:       int32(p.Priority),
		ResumeContext:  p.ResumeContext,
		ToolCallID:     p.ToolCallID,
	}
	if err := s.q.InsertPausedState(ctx, arg); err != nil {
		return fmt.Errorf("insert paused state %s: %w", p.Token, err)
	}
	return nil
}

// GetByToken fetches one pause by token, mapping a missing row to ErrPauseNotFound.
func (s *Store) GetByToken(ctx context.Context, token string) (Pending, error) {
	id, err := parseUUID("token", token)
	if err != nil {
		return Pending{}, fmt.Errorf("get paused state: %w", err)
	}
	row, err := s.q.GetPausedStateByToken(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Pending{}, fmt.Errorf("get paused state %s: %w", token, ErrPauseNotFound)
		}
		return Pending{}, fmt.Errorf("get paused state %s: %w", token, err)
	}
	return fromRow(row), nil
}

// ListPending returns the conversation's still-pending pauses (resumed_at IS NULL)
// in the total FIFO order priority DESC, created_at ASC, token ASC. The token
// tiebreaker is mandatory: rows inserted in one tx share created_at = now()
// (RESEARCH Pitfall 4), so without it the order would be non-deterministic.
func (s *Store) ListPending(ctx context.Context, conversationID string) ([]Pending, error) {
	convID, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	rows, err := s.q.ListPendingPausedStates(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("list pending for %s: %w", conversationID, err)
	}
	out := make([]Pending, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// Record is the richer projection of a paused_states row for the operator-facing
// `aura paused-states list` CLI: it carries the resolution state + the persisted
// resumed_answer (the auto-terminated marker when Loop.Stop closed it), which the
// pending-only Pending projection omits.
type Record struct {
	Token          string
	ConversationID string
	Kind           string
	Question       string
	Priority       int
	Resumed        bool
	ResumedAnswer  string // the {action,content} content, or "" when still pending
}

// ListRecent returns the most-recent paused_states rows (pending + resolved) across
// all conversations, newest first, for the CLI. limit<=0 falls back to 50.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.q.ListRecentPausedStates(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list recent paused states: %w", err)
	}
	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		rec := Record{
			Token:          uuid.UUID(r.Token.Bytes).String(),
			ConversationID: uuid.UUID(r.ConversationID.Bytes).String(),
			Kind:           r.Kind,
			Question:       r.Question,
			Priority:       int(r.Priority),
			Resumed:        r.ResumedAt.Valid,
		}
		if len(r.ResumedAnswer) > 0 {
			var ans ResumeAnswer
			if json.Unmarshal(r.ResumedAnswer, &ans) == nil {
				rec.ResumedAnswer = ans.Content
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// MarkResumed resolves one pause with the AM-02 {action, content} answer. An
// unknown / already-resumed token (zero rows affected) returns ErrPauseNotFound so
// the caller gets a clear error rather than a silent no-op.
func (s *Store) MarkResumed(ctx context.Context, token string, ans ResumeAnswer) error {
	id, err := parseUUID("token", token)
	if err != nil {
		return fmt.Errorf("mark resumed: %w", err)
	}
	answer, err := encodeAnswer(ans)
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	tag, err := s.pool.Exec(ctx, markResumedSQL, id, answer)
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark resumed %s: %w", token, ErrPauseNotFound)
	}
	return nil
}

// markResumedSQL mirrors the sqlc MarkPausedStateResumed query but is issued via
// pool.Exec so the RowsAffected count drives the ErrPauseNotFound classification
// (the generated :exec discards the CommandTag).
const markResumedSQL = `UPDATE aura.paused_states
SET resumed_at = now(), resumed_answer = $2
WHERE token = $1 AND resumed_at IS NULL`

// MarkResumedBatch resolves many pauses atomically (one tx via db.WithTx). Every
// token must resolve a still-pending row; if any token is unknown/already-resumed
// the whole batch rolls back with ErrPauseNotFound (no partial resolution).
func (s *Store) MarkResumedBatch(ctx context.Context, answers map[string]ResumeAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		for token, ans := range answers {
			id, err := parseUUID("token", token)
			if err != nil {
				return fmt.Errorf("mark resumed batch: %w", err)
			}
			answer, err := encodeAnswer(ans)
			if err != nil {
				return fmt.Errorf("mark resumed batch %s: %w", token, err)
			}
			if err := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{
				Token: id, ResumedAnswer: answer,
			}); err != nil {
				return fmt.Errorf("mark resumed batch %s: %w", token, err)
			}
			// The generated :exec discards RowsAffected; re-check existence so an
			// unknown token rolls the batch back rather than silently committing.
			row, err := q.GetPausedStateByToken(ctx, id)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("mark resumed batch %s: %w", token, ErrPauseNotFound)
				}
				return fmt.Errorf("mark resumed batch %s: %w", token, err)
			}
			if !row.ResumedAt.Valid {
				return fmt.Errorf("mark resumed batch %s: %w", token, ErrPauseNotFound)
			}
		}
		return nil
	})
}

// AutoResolveForConversation is the SPEC Req#11 Loop.Stop helper: resolve every
// open pending for a conversation with the auto-terminated marker, atomically. It
// leaves zero resumed_at IS NULL rows for that conversation.
func (s *Store) AutoResolveForConversation(ctx context.Context, conversationID string) error {
	convID, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("auto-resolve: %w", err)
	}
	answer, err := encodeAnswer(ResumeAnswer{Action: ActionCancel, Content: autoTerminatedContent})
	if err != nil {
		return fmt.Errorf("auto-resolve %s: %w", conversationID, err)
	}
	return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		if err := q.AutoResolvePendingForConversation(ctx, sqlc.AutoResolvePendingForConversationParams{
			ConversationID: convID,
			ResumedAnswer:  answer,
		}); err != nil {
			return fmt.Errorf("auto-resolve %s: %w", conversationID, err)
		}
		return nil
	})
}

// CleanupResumedOlderThan deletes resolved rows older than the cutoff (a GC the
// CLI `aura paused-states purge` drives). It never touches pending rows.
func (s *Store) CleanupResumedOlderThan(ctx context.Context, cutoff pgtype.Timestamptz) error {
	if err := s.q.CleanupResumedOlderThan(ctx, cutoff); err != nil {
		return fmt.Errorf("cleanup resumed: %w", err)
	}
	return nil
}

// fromRow projects a generated row onto the domain Pending type.
func fromRow(r sqlc.AuraPausedStates) Pending {
	return Pending{
		Token:          uuid.UUID(r.Token.Bytes).String(),
		ConversationID: uuid.UUID(r.ConversationID.Bytes).String(),
		Kind:           r.Kind,
		Question:       r.Question,
		Options:        r.Options,
		Priority:       int(r.Priority),
		ToolCallID:     r.ToolCallID,
	}
}

// encodeAnswer validates the action and marshals the AM-02 {action, content} jsonb.
func encodeAnswer(ans ResumeAnswer) ([]byte, error) {
	switch ans.Action {
	case ActionAccept, ActionDecline, ActionCancel:
	default:
		return nil, fmt.Errorf("%w: action %q must be accept|decline|cancel", ErrInvalidAnswer, ans.Action)
	}
	b, err := json.Marshal(ans)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrInvalidAnswer, err)
	}
	return b, nil
}

// parseUUID converts a canonical UUID string into the pgtype.UUID the generated
// queries expect (mirrors internal/identity.parseUUID).
func parseUUID(field, s string) (pgtype.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}
