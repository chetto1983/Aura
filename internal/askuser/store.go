// Package askuser is the per-domain Store over aura.paused_states (PRD 1.5). It
// copies the canonical Store pattern proved in internal/identity (D-A4-01):
// Store{pool} over the generated sqlc surface, SQLSTATE-based error
// classification via errors.As + pgErr.Code (never message matching), sentinel
// errors, pgtype conversion at the boundary, and one identity-scoped transaction
// per call (db.WithCallerIdentityTx) since migration 0089 made aura.paused_states
// fail closed.
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

// Store wraps a pgx pool — the canonical shape from internal/identity, minus the
// pool-bound Queries handle it used to carry. aura.paused_states has been fail-closed
// since migration 0089, so a statement that does not set app.current_identity sees no
// pause and can insert none; scoped() is therefore the ONE way this store reaches the
// table, and the *Tx methods hand the work to a transaction the caller has already scoped.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// scoped runs fn inside a transaction bound to the identity on ctx — the D-07 RLS carrier.
// See db.WithCallerIdentityTx for where that identity comes from at each entry point.
func (s *Store) scoped(ctx context.Context, fn func(*sqlc.Queries) error) error {
	return db.WithCallerIdentityTx(ctx, s.pool, fn)
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
	arg, err := insertArgs(p)
	if err != nil {
		return err
	}
	if err := q.InsertPausedState(ctx, arg); err != nil {
		return fmt.Errorf("insert paused state %s: %w", p.Token, err)
	}
	return nil
}

// insertArgs validates and projects InsertParams onto the generated params. It is separate
// from the write so Insert can reject malformed input BEFORE opening a transaction — the
// parse-before-DB contract TestStore_BadUUID_FailsBeforeDB pins with a nil-pool Store.
func insertArgs(p InsertParams) (sqlc.InsertPausedStateParams, error) {
	token, err := db.ParseUUID("token", p.Token)
	if err != nil {
		return sqlc.InsertPausedStateParams{}, fmt.Errorf("insert paused state: %w", err)
	}
	convID, err := db.ParseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return sqlc.InsertPausedStateParams{}, fmt.Errorf("insert paused state: %w", err)
	}
	var proxiedChild pgtype.Text
	if p.ProxiedFromChildID != nil {
		proxiedChild = pgtype.Text{String: *p.ProxiedFromChildID, Valid: true}
	}
	return sqlc.InsertPausedStateParams{
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
	}, nil
}

// Insert persists one pending pause in a transaction scoped to the identity on ctx. It is a
// thin wrapper over InsertTx; the parse-before-DB guard in InsertTx still short-circuits
// malformed input before any pool round-trip. The scoping is not decoration: the 0032
// BEFORE INSERT trigger fills identity_id by SELECTing the parent conversation, and that
// lookup is itself owner-filtered, so without the identity the trigger finds nothing and
// the insert is refused.
func (s *Store) Insert(ctx context.Context, p InsertParams) error {
	arg, err := insertArgs(p)
	if err != nil {
		return err
	}
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		return q.InsertPausedState(ctx, arg)
	}); err != nil {
		return fmt.Errorf("insert paused state %s: %w", p.Token, err)
	}
	return nil
}

// GetByToken fetches one pause by token, scoped to the identity on ctx, mapping a missing
// row — unknown token OR a token owned by someone else, which RLS makes indistinguishable —
// to ErrPauseNotFound.
func (s *Store) GetByToken(ctx context.Context, token string) (Pending, error) {
	id, err := db.ParseUUID("token", token)
	if err != nil {
		return Pending{}, fmt.Errorf("get paused state: %w", err)
	}
	var pending Pending
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		row, gErr := q.GetPausedStateByToken(ctx, id)
		if gErr != nil {
			if errors.Is(gErr, pgx.ErrNoRows) {
				return ErrPauseNotFound
			}
			return gErr
		}
		pending = fromRow(row)
		return nil
	}); err != nil {
		return Pending{}, fmt.Errorf("get paused state %s: %w", token, err)
	}
	return pending, nil
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
	var out []Pending
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, lErr := q.ListPendingPausedStates(ctx, convID)
		if lErr != nil {
			return lErr
		}
		out = make([]Pending, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromRow(r))
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list pending for %s: %w", conversationID, err)
	}
	return out, nil
}

// ListPendingAll returns the still-pending pauses ACROSS ALL of the ctx identity's
// conversations (resumed_at IS NULL) in the same total FIFO order as ListPending — priority
// DESC, created_at ASC, token ASC — capped at limit (APRV-01 / D-04, the approval center's
// cross-thread read). "All" means all THREADS, not all tenants: the query carries no owner
// predicate and RLS is what scopes it, which is the same backstop
// ListPendingAllForIdentity's explicit predicate sits on top of. The token tiebreaker is
// mandatory for the same reason as ListPending: tx-batched rows share created_at = now()
// (RESEARCH Pitfall 4), so without it the cross-thread order would be non-deterministic.
// limit<=0 falls back to 100 (mirroring ListRecent's <=0 guard). It reuses the SAME fromRow
// projector, so each Pending carries Question/Options/Priority/Kind/ConversationID and the
// source thread (ProxiedFromChildID) the operator needs to jump to the originating thread.
func (s *Store) ListPendingAll(ctx context.Context, limit int) ([]Pending, error) {
	lim := normalizeLimit(limit, 100)
	var out []Pending
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, lErr := q.ListAllPendingPausedStates(ctx, lim)
		if lErr != nil {
			return lErr
		}
		out = make([]Pending, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromRow(r))
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list pending (all conversations): %w", err)
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

// ListRecent returns the most-recent paused_states rows (pending + resolved) across all of
// the ctx identity's conversations, newest first, for the CLI. limit<=0 falls back to 50.
// `aura paused-states list` resolves the operator identity onto ctx before calling
// (cmd/aura/paused_states.go) — with no identity the table is invisible, by design.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Record, error) {
	lim := normalizeLimit(limit, 50)
	var out []Record
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, lErr := q.ListRecentPausedStates(ctx, lim)
		if lErr != nil {
			return lErr
		}
		out = make([]Record, 0, len(rows))
		for _, r := range rows {
			rec := Record{
				Token:          uuid.UUID(r.Token.Bytes).String(),
				ConversationID: uuid.UUID(r.ConversationID.Bytes).String(),
				Kind:           r.Kind,
				Question:       r.Question,
				Priority:       int(r.Priority),
				Resumed:        r.ResumedAt.Valid,
			}
			answer, dErr := decodeResumedAnswer(rec.Token, r.ResumedAnswer)
			if dErr != nil {
				return dErr
			}
			rec.ResumedAnswer = answer
			out = append(out, rec)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list recent paused states: %w", err)
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

// MarkResumed resolves one pause with the AM-02 {action, content} answer, in a transaction
// scoped to the identity on ctx. A thin wrapper over MarkResumedTx; the parse/encode guards
// short-circuit malformed input before any pool round-trip. An unknown, already-resumed, or
// foreign token all return ErrPauseNotFound — under fail-closed RLS the conditional UPDATE
// matches 0 rows in every one of those cases, which is the same answer for the same reason.
func (s *Store) MarkResumed(ctx context.Context, token string, ans ResumeAnswer) error {
	id, err := db.ParseUUID("token", token)
	if err != nil {
		return fmt.Errorf("mark resumed: %w", err)
	}
	answer, err := encodeAnswer(ans)
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		n, mErr := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
		if mErr != nil {
			return mErr
		}
		if n == 0 {
			return ErrPauseNotFound
		}
		return nil
	}); err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	return nil
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

// MarkResumedBatch resolves many pauses atomically (one identity-scoped tx over
// MarkResumedBatchTx). Every token must resolve a still-pending row; if any token is
// unknown/already-resumed the whole batch rolls back with ErrPauseNotFound (no partial
// resolution). An empty map is a no-op that opens no transaction.
func (s *Store) MarkResumedBatch(ctx context.Context, answers map[string]ResumeAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	return s.scoped(ctx, func(q *sqlc.Queries) error {
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
	return s.scoped(ctx, func(q *sqlc.Queries) error {
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
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		return q.CleanupResumedOlderThan(ctx, cutoff)
	}); err != nil {
		return fmt.Errorf("cleanup resumed: %w", err)
	}
	return nil
}

// normalizeLimit narrows a caller-supplied limit onto the int32 the generated LIMIT binds
// (QUAL-04a / D-15a). A non-positive OR int32-overflowing limit falls back to fallback, so
// int32(limit) can never wrap to a negative LIMIT — the conversion happens only inside the
// proven-safe branch (CodeQL go/incorrect-integer-conversion). ListPendingAll and ListRecent
// share it because they had the identical guard with different defaults.
func normalizeLimit(limit int, fallback int32) int32 {
	if limit > 0 && limit <= math.MaxInt32 {
		return int32(limit)
	}
	return fallback
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
