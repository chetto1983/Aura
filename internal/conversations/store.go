// Package conversations is the per-domain Store over aura.conversations +
// aura.conversation_turns + aura.context_rot_events (PRD 1.8 / 1.8.5). It copies
// the canonical Store pattern proved in internal/identity and reused by
// internal/askuser (D-A4-01): Store{pool,q} over the generated sqlc surface,
// SQLSTATE-based error classification via errors.As + pgErr.Code (never message
// matching), sentinel errors, pgtype boundary conversion (Pitfall 5), and
// db.WithTx for atomic multi-statement writes.
//
// Scope is the durable conversation core (SPEC Req#7-13): multi-thread
// persistence, the atomic per-turn AppendTurn (INSERT turn + UPDATE aggregates in
// ONE tx — SC-2), byte-identical LoadHistory (Req#8), sidecar spill for oversized
// turn content, per-conversation token+USD aggregation, status/rename/delete, and
// the locked cross-slice SearchConversationTurns FTS query (Req#13). The L1/L2/L2.5
// context ladder lives in context.go; the auto-title worker body in title.go; the
// boot reconciliation GC in orphan_scan.go. The Runner (04-05) consumes a narrow
// interface; no interface is declared here (D-A2-02, "accept interfaces, return
// structs").
package conversations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status values mirror the aura.conversations CHECK constraint. archive/unarchive
// flip between active and archived; UpdateStatus(deleted) is the soft-delete that
// hides a conversation from List without a row delete.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
	StatusDeleted  = "deleted"

	// DefaultTurnCapBytes is the default inline turn-content ceiling.
	DefaultTurnCapBytes = 64 * 1024
)

// ErrConversationNotFound is a missing conversation lookup — a sentinel so callers
// classify the failure without string matching.
var ErrConversationNotFound = errors.New("conversation not found")

// ConversationCleaner is the narrow purge surface Delete calls to tear down a
// conversation's on-disk tree (workspace/ AND the .result spillover) with an
// os.Root no-follow cascade (D-14). It is DECLARED HERE — the consumer — so the
// concrete impl (sandbox.WorkspaceManager) can be injected by the composition
// root (main.go / chat boot) WITHOUT conversations importing sandbox: that would
// be an import cycle (sandbox already declares a matching shape sandbox-side only
// for a compile-time assertion, never importing us — landmine #4).
type ConversationCleaner interface {
	PurgeConversationDir(convID string) error
}

// Store wraps a pgx pool and the generated Queries — the canonical shape from
// internal/identity. runDir is the $AURA_RUN_DIR root the sidecar spill writes
// under; turnCapBytes is AURA_CONVERSATION_TURN_CAP_BYTES (content over this
// spills to a sidecar file). cleaner is the optional os.Root cascade injected at
// boot; when nil, Delete falls back to os.RemoveAll (the pre-2b behavior).
//
// Every statement against aura.conversations and aura.conversation_turns goes
// through scoped(), never through q: migration 0089 made both tables fail closed,
// so a query outside that seam sees nothing and writes nothing. q is what is LEFT
// of the old pool-bound surface — the identity-less query handle, kept for the one
// read that legitimately has no owner (enumerating aura.identities, which carries
// no RLS, so the cross-tenant sweeps can iterate owners).
type Store struct {
	pool         *pgxpool.Pool
	q            *sqlc.Queries
	scopedTx     func(context.Context, string, func(*sqlc.Queries) error) error
	runDir       string
	turnCapBytes int
	cleaner      ConversationCleaner
}

// scoped runs fn inside a transaction bound to the identity on ctx — the D-07 RLS
// carrier, and the store's ONE way to reach conversation data. See
// db.WithCallerIdentityTx for where that identity comes from at each entry point.
//
// It goes through the scopedTx field rather than calling db.WithCallerIdentityTx directly so
// the package's DB-free unit tests keep a seam to drive the error branches through: pool.Begin
// needs a concrete *pgxpool.Pool, which no fake can satisfy. New always installs the real
// carrier; a Store built any other way is a test double.
func (s *Store) scoped(ctx context.Context, fn func(*sqlc.Queries) error) error {
	return s.scopedTx(ctx, identityctx.IdentityID(ctx), fn)
}

// ownerIdentities lists every identity id, for the two sweeps that must act on behalf of
// every tenant (ScanOrphans and ListReservedDeletes). aura.identities carries no RLS — it is
// the login lookup — so this read needs no principal, and running one identity-scoped
// transaction per owner is what lets those sweeps stay cross-tenant WITHOUT an RLS bypass
// or a service role. The identity count is small; this is N small queries, not N per row.
func (s *Store) ownerIdentities(ctx context.Context) ([]string, error) {
	return listOwnerIdentities(ctx, s.q)
}

// listOwnerIdentities is the free-function half of ownerIdentities, shared with ScanOrphans
// (which is a package function over a bare pool, not a Store method).
func listOwnerIdentities(ctx context.Context, q *sqlc.Queries) ([]string, error) {
	rows, err := q.ListIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list owner identities: %w", err)
	}
	owners := make([]string, 0, len(rows))
	for _, r := range rows {
		owners = append(owners, uuid.UUID(r.ID.Bytes).String())
	}
	return owners, nil
}

// Config carries the Store's filesystem + spill knobs (from config.Config).
// Cleaner is the optional os.Root cascade (sandbox.WorkspaceManager) the
// composition root injects so Delete tears down the per-conversation tree
// symlink-safely; a nil Cleaner keeps the os.RemoveAll fallback.
type Config struct {
	RunDir       string // $AURA_RUN_DIR — sidecar root
	TurnCapBytes int    // AURA_CONVERSATION_TURN_CAP_BYTES
	Cleaner      ConversationCleaner
}

// New builds a Store over an open pool. A zero TurnCapBytes falls back to the
// SPEC default so a misconfigured caller never disables spillover entirely.
func New(pool *pgxpool.Pool, cfg Config) *Store {
	cap := cfg.TurnCapBytes
	if cap <= 0 {
		cap = DefaultTurnCapBytes
	}
	s := &Store{pool: pool, q: sqlc.New(pool), runDir: cfg.RunDir, turnCapBytes: cap, cleaner: cfg.Cleaner}
	s.scopedTx = func(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
		return db.WithIdentityTx(ctx, pool, identityID, fn)
	}
	return s
}

// Conversation is the domain projection of aura.conversations — plain Go types at
// the package boundary instead of the sqlc pgtype wrappers. Title is the rendered
// title (NULL renders "(untitled <created_at>)" via DisplayTitle).
type Conversation struct {
	ID         string
	Title      string // empty when the DB title is NULL
	TitleSet   bool   // false when the DB title is NULL
	IdentityID string
	Status     string
	Model      string
	// ReasoningEffort is the per-conversation effort symbol persisted in the metadata
	// jsonb (Phase 37E / D-06); "" means absent → the frontend hydrates it as auto (the
	// adaptive policy runs unchanged, D-07). Populated by conversationFromRow.
	ReasoningEffort string
	// SnapshotVersion is an internal monotonic export-delete concurrency token.
	// It is omitted from conversation.json; the exporter carries it separately.
	SnapshotVersion   int64 `json:"-"`
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCachedTokens int64
	// LastInputTokens is the input_tokens of the most recent request-bearing turn —
	// the CURRENT context-window fill, NOT the cumulative TotalInputTokens (which sums
	// every turn and dwarfs the window on a long chat). The runtime footer's context
	// gauge reads it so a reloaded conversation shows real fill. Populated ONLY by
	// GetForIdentity (the identity-scoped read behind GET /api/conversations/{id}); the
	// unscoped Get leaves it 0 — no gauge consumer reads that path, and it keeps the hot
	// ownership/search Get a single query. 0 when the conversation has no request yet.
	LastInputTokens int64
	TotalCostUSD    float64
	CreatedAt       string // RFC3339; used by DisplayTitle for the untitled fallback
}

// Turn is the domain projection of one aura.conversation_turns row, ready to be
// re-hydrated into an llm.Message by LoadHistory. ToolCalls is the raw jsonb.
type Turn struct {
	Seq                int
	Role               string
	Content            string
	ContentSidecarPath string
	ToolCallID         string
	ToolCalls          []byte
	InputTokens        int
	OutputTokens       int
	CachedTokens       int
}

// CreateParams carries the inputs for a new conversation.
type CreateParams struct {
	ID         string
	IdentityID string
	Model      string
	Metadata   []byte
}

// Create persists a new active conversation and returns its projection.
//
// It scopes to p.IdentityID, NOT to the context: Create is the statement that DECIDES
// ownership, so the owner is an input here rather than ambient caller state. The caller is
// the authority — runner.defaultConversationOwner validates the authenticated principal via
// GetIdentityByID before it ever reaches this method.
func (s *Store) Create(ctx context.Context, p CreateParams) (Conversation, error) {
	id, err := db.ParseUUID("id", p.ID)
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	identityID, err := db.ParseUUID("identity_id", p.IdentityID)
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	var conv Conversation
	if err := s.scopedTx(ctx, p.IdentityID, func(q *sqlc.Queries) error {
		row, cErr := q.CreateConversation(ctx, sqlc.CreateConversationParams{
			ID:         id,
			IdentityID: identityID,
			Model:      p.Model,
			Metadata:   p.Metadata,
		})
		if cErr != nil {
			return fmt.Errorf("create conversation %s: %w", p.ID, cErr)
		}
		conv = conversationFromRow(row)
		return nil
	}); err != nil {
		return Conversation{}, err
	}
	return conv, nil
}

// Get fetches one conversation owned by the identity on ctx, mapping a missing row —
// unknown id OR an id owned by someone else, which RLS makes indistinguishable — to
// ErrConversationNotFound.
func (s *Store) Get(ctx context.Context, conversationID string) (Conversation, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	var conv Conversation
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		row, gErr := q.GetConversation(ctx, id)
		if gErr != nil {
			if errors.Is(gErr, pgx.ErrNoRows) {
				return fmt.Errorf("get conversation %s: %w", conversationID, ErrConversationNotFound)
			}
			return fmt.Errorf("get conversation %s: %w", conversationID, gErr)
		}
		conv = conversationFromRow(row)
		return nil
	}); err != nil {
		return Conversation{}, err
	}
	return conv, nil
}

// List returns the ctx identity's conversations ordered by last_active_at DESC.
// includeArchived adds archived rows; deleted rows are always excluded (the query filters
// status). The query itself carries no owner predicate — RLS is what scopes it.
func (s *Store) List(ctx context.Context, includeArchived bool) ([]Conversation, error) {
	var out []Conversation
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, lErr := q.ListConversations(ctx, includeArchived)
		if lErr != nil {
			return fmt.Errorf("list conversations: %w", lErr)
		}
		out = make([]Conversation, 0, len(rows))
		for _, r := range rows {
			out = append(out, conversationFromRow(r))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus moves a conversation between active/archived/deleted. It validates
// the target status before the DB round-trip.
func (s *Store) UpdateStatus(ctx context.Context, conversationID, status string) error {
	switch status {
	case StatusActive, StatusArchived, StatusDeleted:
	default:
		return fmt.Errorf("update status: invalid status %q", status)
	}
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return s.scoped(ctx, func(q *sqlc.Queries) error {
		if uErr := q.UpdateConversationStatus(ctx, sqlc.UpdateConversationStatusParams{ID: id, Status: status}); uErr != nil {
			return fmt.Errorf("update status %s -> %s: %w", conversationID, status, uErr)
		}
		return nil
	})
}

// setTitle folds Rename and SetTitleIfNull. Both parse the id, open ONE identity-scoped
// transaction and run a single title statement; only the statement differs, so the
// statement is the argument. Keeping them as two copies is how a scoping fix gets applied
// to one and forgotten on the other.
func (s *Store) setTitle(
	ctx context.Context,
	op, conversationID, title string,
	run func(*sqlc.Queries, pgtype.UUID, pgtype.Text) error,
) error {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return s.scoped(ctx, func(q *sqlc.Queries) error {
		if rErr := run(q, id, pgtype.Text{String: title, Valid: true}); rErr != nil {
			return fmt.Errorf("%s %s: %w", op, conversationID, rErr)
		}
		return nil
	})
}

// Rename sets the conversation title unconditionally (the CLI `aura chat rename`).
func (s *Store) Rename(ctx context.Context, conversationID, title string) error {
	return s.setTitle(ctx, "rename", conversationID, title,
		func(q *sqlc.Queries, id pgtype.UUID, t pgtype.Text) error {
			return q.RenameConversation(ctx, sqlc.RenameConversationParams{ID: id, Title: t})
		})
}

// SetTitleIfNull idempotently sets the title only when it is still NULL (the
// auto-title worker drives this — D-A5-01). A repeat call after the title is set
// is a no-op (the WHERE title IS NULL filters it).
func (s *Store) SetTitleIfNull(ctx context.Context, conversationID, title string) error {
	return s.setTitle(ctx, "set title", conversationID, title,
		func(q *sqlc.Queries, id pgtype.UUID, t pgtype.Text) error {
			return q.SetConversationTitleIfNull(ctx, sqlc.SetConversationTitleIfNullParams{ID: id, Title: t})
		})
}

// scopedSeq folds the single-integer reads — CountTurns here and CanonicalBranchLeaf in
// store_branch.go. Same shape down to the error wrap: parse, one scoped transaction, one
// query, widen to int.
func scopedSeq[T ~int32 | ~int64](
	ctx context.Context,
	s *Store,
	op, field, conversationID string,
	run func(*sqlc.Queries, pgtype.UUID) (T, error),
) (int, error) {
	id, err := db.ParseUUID(field, conversationID)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	var value T
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		got, qErr := run(q, id)
		if qErr != nil {
			return fmt.Errorf("%s %s: %w", op, conversationID, qErr)
		}
		value = got
		return nil
	}); err != nil {
		return 0, err
	}
	return int(value), nil
}

// CountTurns returns the number of persisted turns for a conversation. The Runner
// uses it for non-fatal bookkeeping such as auto-title eligibility; append sequence
// allocation happens inside AppendTurn's transaction.
func (s *Store) CountTurns(ctx context.Context, conversationID string) (int, error) {
	return scopedSeq(ctx, s, "count turns", "id", conversationID,
		func(q *sqlc.Queries, id pgtype.UUID) (int64, error) { return q.CountTurns(ctx, id) })
}

// LoadHistory reconstructs the loop history from conversation_turns ORDER BY seq.
// Two consecutive calls return byte-identical slices (Req#8): the reconstruction
// is a pure function of the persisted rows (sidecar-spilled content is re-read
// from disk; no nondeterministic source feeds the result). The returned slice is
// raw — the L1/L2/L2.5 ladder (context.go) is applied by the caller around it.
func (s *Store) LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error) {
	turns, err := s.loadTurns(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg, err := turnToMessage(t)
		if err != nil {
			return nil, fmt.Errorf("load history %s seq %d: %w", conversationID, t.Seq, err)
		}
		out = append(out, msg)
	}
	return repairToolMessagePairs(out), nil
}

// loadTurns fetches the ordered Turn projections, rehydrating sidecar-spilled
// content from disk. Shared by LoadHistory and the context ladder.
func (s *Store) loadTurns(ctx context.Context, conversationID string) ([]Turn, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("load turns: %w", err)
	}
	var rows []sqlc.ListTurnsBySeqRow
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		listed, lErr := q.ListTurnsBySeq(ctx, id)
		if lErr != nil {
			return fmt.Errorf("load turns %s: %w", conversationID, lErr)
		}
		rows = listed
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]Turn, 0, len(rows))
	for _, r := range rows {
		t := turnFromRow(r)
		if t.ContentSidecarPath != "" {
			data, rerr := s.readTurnSidecar(conversationID, t.Seq)
			if rerr != nil {
				return nil, fmt.Errorf("load turns %s seq %d: read sidecar: %w",
					conversationID, t.Seq, rerr)
			}
			t.Content = string(data)
		}
		out = append(out, t)
	}
	return out, nil
}

// Delete removes the conversation row (conversation_turns + paused_states cascade
// via FK ON DELETE CASCADE), then tears down the per-conversation sidecar dir
// AFTER the DB delete commits. When a Cleaner is wired (2b) the teardown is the
// os.Root no-follow cascade (sandbox.WorkspaceManager.PurgeConversationDir) over
// the WHOLE <id>/ tree — workspace/ AND the .result spillover — so an attacker-
// planted symlink in the sidecar-writable workspace is unlinked as a LINK, never
// followed to its target (ROADMAP crit 2, landmine #4). With no Cleaner it falls
// back to os.RemoveAll. A filesystem failure is a WARN-level degradation (returned
// as an error the caller may log), NOT a rolled-back delete — the boot orphan scan
// reconciles a leftover dir at next start (SPEC Req#12).
func (s *Store) Delete(ctx context.Context, conversationID string) error {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		if dErr := q.DeleteConversation(ctx, id); dErr != nil {
			return fmt.Errorf("delete conversation %s: %w", conversationID, dErr)
		}
		return nil
	}); err != nil {
		return err
	}
	return s.purgeConversationArtifacts(conversationID)
}

// purgeConversationArtifacts tears down the per-conversation on-disk tree AFTER the DB
// row is deleted: the os.Root no-follow cascade when a Cleaner is wired (2b), else the
// os.RemoveAll fallback. Shared by Delete and DeleteForIdentity (Phase 36) so both hard
// deletes reclaim the sidecar dir identically. A filesystem failure is a WARN-level
// degradation the caller may surface — the boot orphan scan reconciles a leftover dir,
// NEVER a rolled-back delete (the DB row is already gone).
func (s *Store) purgeConversationArtifacts(conversationID string) error {
	if s.cleaner != nil {
		// validateID here too: the cleaner's own guard duplicates it, but rejecting
		// a traversal-shaped id before handing it across the seam keeps the contract
		// explicit at the call site (D-26: session_id == conversation_id).
		if vErr := validateID("conversation_id", conversationID); vErr != nil {
			return fmt.Errorf("delete conversation %s: %w", conversationID, vErr)
		}
		if pErr := s.cleaner.PurgeConversationDir(conversationID); pErr != nil {
			return fmt.Errorf("delete conversation %s: purge dir (orphan-scan will reconcile): %w",
				conversationID, pErr)
		}
		return nil
	}
	dir, err := s.sidecarDir(conversationID)
	if err != nil {
		// The DB row is gone; a malformed id here cannot happen (parseUUID passed),
		// but guard anyway — report without failing the committed delete.
		return fmt.Errorf("delete conversation %s: sidecar dir: %w", conversationID, err)
	}
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return fmt.Errorf("delete conversation %s: remove sidecar dir (orphan-scan will reconcile): %w",
			conversationID, rmErr)
	}
	return nil
}

// sidecarDir is the per-conversation sidecar directory
// ($AURA_RUN_DIR/conversations/<id>), validated against path traversal (D-26:
// session_id == conversation_id).
func (s *Store) sidecarDir(conversationID string) (string, error) {
	if err := validateID("conversation_id", conversationID); err != nil {
		return "", err
	}
	return filepath.Join(s.runDir, "conversations", conversationID), nil
}
