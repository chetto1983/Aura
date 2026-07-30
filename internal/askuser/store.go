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
	"math"
	"sort"

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
// ProxiedFromChildID is the flat swarm-worker id ("w1".."wN", D-05/D-15) the pause
// proxies for, or "" for a direct (non-proxied) call — it is the APRV-01 "source
// thread" the cross-thread approval list shows so the operator knows which (possibly
// background) thread raised the interrupt.
type Pending struct {
	Token              string
	ConversationID     string
	Kind               string
	Question           string
	Options            json.RawMessage
	Priority           int
	ToolCallID         string
	ResumeContext      json.RawMessage
	ProxiedFromChildID string
}

// ResumeAnswer is the AM-02 resolution payload persisted as resumed_answer jsonb.
type ResumeAnswer struct {
	Action  string `json:"action"`
	Content string `json:"content"`
}

// InsertParams carries the plain fields for one new pause. NEVER a tools type
// (D-A1-04): the caller (the Runner, observing the pause Event) supplies these.
// ProxiedFromChildID/ProxiedToolCallID are the optional D-05 swarm-relay ids: a
// nil child id (and an empty tool_call id) persist as SQL NULL for direct calls.
type InsertParams struct {
	Token              string
	ConversationID     string
	Kind               string
	Question           string
	Options            json.RawMessage
	Priority           int
	ToolCallID         string
	ResumeContext      json.RawMessage
	ProxiedFromChildID *string
	ProxiedToolCallID  string
}

// InsertTx persists one pending pause using the caller-supplied Queries (bound to the
// caller's transaction — it opens NO transaction of its own). The 34-06 HITL
// ResumeCommitter uses it to span a pause write and a resume/answer turn append in one
// cross-store db.WithTx (D-03). Options/ResumeContext are jsonb (nil → SQL NULL). The
// proxied_* columns carry the D-05 relay ids when the pause proxies a child's
// needs_user_input report; they stay NULL for direct calls (a nil child id leaves the
// pgtype.Text Valid:false, an empty tool_call id leaves the pgtype.Text Valid:false).
// proxied_from_child_id is the flat worker id ("w1".."wN", D-15/D-16) stored verbatim
// as text — NOT a uuid (CR-01): the swarm report carries no uuid for a model to relay,
// so parsing one here would fail the documented happy path.
func (s *Store) InsertTx(ctx context.Context, q *sqlc.Queries, p InsertParams) error {
	token, err := db.ParseUUID("token", p.Token)
	if err != nil {
		return fmt.Errorf("insert paused state: %w", err)
	}
	convID, err := db.ParseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return fmt.Errorf("insert paused state: %w", err)
	}
	var proxiedChild pgtype.Text
	if p.ProxiedFromChildID != nil {
		proxiedChild = pgtype.Text{String: *p.ProxiedFromChildID, Valid: true}
	}
	arg := sqlc.InsertPausedStateParams{
		Token:              token,
		ConversationID:     convID,
		Kind:               p.Kind,
		Question:           p.Question,
		Options:            p.Options,
		Priority:           int32(p.Priority),
		ResumeContext:      p.ResumeContext,
		ToolCallID:         p.ToolCallID,
		ProxiedFromChildID: proxiedChild,
		ProxiedToolCallID:  pgtype.Text{String: p.ProxiedToolCallID, Valid: p.ProxiedToolCallID != ""},
	}
	if err := q.InsertPausedState(ctx, arg); err != nil {
		return fmt.Errorf("insert paused state %s: %w", p.Token, err)
	}
	return nil
}

// Insert persists one pending pause. It is a thin wrapper over InsertTx bound to the
// pool's Queries (s.q): a single INSERT auto-commits, so — matching the pre-34-05
// behavior exactly — it opens no explicit transaction, and the parse-before-DB guard
// in InsertTx still short-circuits malformed input before any pool round-trip.
func (s *Store) Insert(ctx context.Context, p InsertParams) error {
	return s.InsertTx(ctx, s.q, p)
}

// GetByToken fetches one pause by token, mapping a missing row to ErrPauseNotFound.
func (s *Store) GetByToken(ctx context.Context, token string) (Pending, error) {
	id, err := db.ParseUUID("token", token)
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
	convID, err := db.ParseUUID("conversation_id", conversationID)
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

// ListPendingAll returns the still-pending pauses ACROSS ALL conversations
// (resumed_at IS NULL) in the same total FIFO order as ListPending — priority DESC,
// created_at ASC, token ASC — capped at limit (APRV-01 / D-04, the approval center's
// cross-thread read). The token tiebreaker is mandatory for the same reason as
// ListPending: tx-batched rows share created_at = now() (RESEARCH Pitfall 4), so
// without it the cross-thread order would be non-deterministic. limit<=0 falls back
// to 100 (mirroring ListRecent's <=0 guard). It reuses the SAME fromRow projector, so
// each Pending carries Question/Options/Priority/Kind/ConversationID and the source
// thread (ProxiedFromChildID) the operator needs to jump to the originating thread.
func (s *Store) ListPendingAll(ctx context.Context, limit int) ([]Pending, error) {
	// Convert inside the proven-safe branch so the int32 narrowing is guarded at the
	// conversion site (CodeQL go/incorrect-integer-conversion); a non-positive or
	// overflowing limit falls back to the 100 default (mirroring ListRecent).
	var lim int32 = 100
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	rows, err := s.q.ListAllPendingPausedStates(ctx, lim)
	if err != nil {
		return nil, fmt.Errorf("list pending (all conversations): %w", err)
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
	// Guard the int32 narrowing (QUAL-04a / D-15a) mirroring ListPendingAll: a
	// non-positive OR int32-overflowing limit falls back to the 50 default so
	// int32(limit) can never wrap to a negative LIMIT (CodeQL go/incorrect-integer-conversion).
	var lim int32 = 50
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	rows, err := s.q.ListRecentPausedStates(ctx, lim)
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
		answer, err := decodeResumedAnswer(rec.Token, r.ResumedAnswer)
		if err != nil {
			return nil, fmt.Errorf("list recent paused states: %w", err)
		}
		rec.ResumedAnswer = answer
		out = append(out, rec)
	}
	return out, nil
}

// MarkResumedTx resolves one pause with the AM-02 {action, content} answer using the
// caller-supplied Queries (bound to the caller's transaction — it opens NO transaction
// of its own). It maps a rows-affected==0 claim (an unknown / already-resumed token —
// the WHERE resumed_at IS NULL predicate matched no row) to ErrPauseNotFound via the
// regenerated MarkPausedStateResumed (:execrows). The 34-06 ResumeCommitter calls it to
// claim a pause in the SAME tx as the resume-answer turn append (D-03), so a claim and
// its answer commit all-or-nothing.
func (s *Store) MarkResumedTx(ctx context.Context, q *sqlc.Queries, token string, ans ResumeAnswer) error {
	id, err := db.ParseUUID("token", token)
	if err != nil {
		return fmt.Errorf("mark resumed: %w", err)
	}
	answer, err := encodeAnswer(ans)
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	n, err := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	if n == 0 {
		return fmt.Errorf("mark resumed %s: %w", token, ErrPauseNotFound)
	}
	return nil
}

// MarkResumed resolves one pause with the AM-02 {action, content} answer. A thin
// wrapper over MarkResumedTx bound to the pool's Queries (s.q): a single conditional
// UPDATE auto-commits, so — matching the pre-34-05 behavior — it opens no explicit
// transaction, and the parse/encode guards short-circuit malformed input before any
// pool round-trip. An unknown / already-resumed token returns ErrPauseNotFound.
func (s *Store) MarkResumed(ctx context.Context, token string, ans ResumeAnswer) error {
	return s.MarkResumedTx(ctx, s.q, token, ans)
}

// MarkResumedBatchTx resolves many pauses using the caller-supplied Queries (bound to
// the caller's transaction — it opens NO transaction of its own; the caller's db.WithTx
// makes it all-or-nothing). It claims the pauses in SORTED token order so two
// concurrent overlapping batches always lock rows in the same order and cannot deadlock
// (Postgres 40P01, RESEARCH landmine #2 / T-34-B): the loser blocks on the row lock,
// re-evaluates the WHERE resumed_at IS NULL predicate under READ COMMITTED against the
// now-committed row, matches 0 rows, and gets a clean ErrPauseNotFound (→ its tx rolls
// back). Every token must resolve a still-pending row; the first unknown/already-resumed
// token aborts the whole batch with ErrPauseNotFound (no partial resolution).
func (s *Store) MarkResumedBatchTx(ctx context.Context, q *sqlc.Queries, answers map[string]ResumeAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(answers))
	for token := range answers {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	for _, token := range tokens {
		id, err := db.ParseUUID("token", token)
		if err != nil {
			return fmt.Errorf("mark resumed batch: %w", err)
		}
		answer, err := encodeAnswer(answers[token])
		if err != nil {
			return fmt.Errorf("mark resumed batch %s: %w", token, err)
		}
		n, err := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
		if err != nil {
			return fmt.Errorf("mark resumed batch %s: %w", token, err)
		}
		if n == 0 {
			return fmt.Errorf("mark resumed batch %s: %w", token, ErrPauseNotFound)
		}
	}
	return nil
}

// MarkResumedBatch resolves many pauses atomically (one tx via db.WithTx over
// MarkResumedBatchTx). Every token must resolve a still-pending row; if any token is
// unknown/already-resumed the whole batch rolls back with ErrPauseNotFound (no partial
// resolution). An empty map is a no-op that opens no transaction.
func (s *Store) MarkResumedBatch(ctx context.Context, answers map[string]ResumeAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		return s.MarkResumedBatchTx(ctx, q, answers)
	})
}

// AutoResolveForConversation is the SPEC Req#11 Loop.Stop helper: resolve every
// open pending for a conversation with the auto-terminated marker, atomically. It
// leaves zero resumed_at IS NULL rows for that conversation.
func (s *Store) AutoResolveForConversation(ctx context.Context, conversationID string) error {
	convID, err := db.ParseUUID("conversation_id", conversationID)
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

// fromRow projects a generated row onto the domain Pending type. A NULL
// proxied_from_child_id (a direct, non-proxied call) projects to the empty string.
func fromRow(r sqlc.AuraPausedStates) Pending {
	var proxiedChild string
	if r.ProxiedFromChildID.Valid {
		proxiedChild = r.ProxiedFromChildID.String
	}
	return Pending{
		Token:              uuid.UUID(r.Token.Bytes).String(),
		ConversationID:     uuid.UUID(r.ConversationID.Bytes).String(),
		Kind:               r.Kind,
		Question:           r.Question,
		Options:            r.Options,
		Priority:           int(r.Priority),
		ToolCallID:         r.ToolCallID,
		ResumeContext:      append(json.RawMessage(nil), r.ResumeContext...),
		ProxiedFromChildID: proxiedChild,
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

func decodeResumedAnswer(token string, raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var ans ResumeAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return "", fmt.Errorf("decode resumed_answer for %s: %w", token, err)
	}
	return ans.Content, nil
}
