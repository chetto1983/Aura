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
	"path"
	"path/filepath"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
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
// internal/identity. Non-tx reads/writes use s.q; the atomic per-turn write
// (AppendTurn) and delete-then-rm wrap db.WithTx / pool ops. runDir is the
// $AURA_RUN_DIR root the sidecar spill writes under; turnCapBytes is
// AURA_CONVERSATION_TURN_CAP_BYTES (content over this spills to a sidecar file).
// cleaner is the optional os.Root cascade injected at boot; when nil, Delete
// falls back to os.RemoveAll (the pre-2b behavior).
type Store struct {
	pool         *pgxpool.Pool
	q            *sqlc.Queries
	runDir       string
	turnCapBytes int
	cleaner      ConversationCleaner
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
	return &Store{pool: pool, q: sqlc.New(pool), runDir: cfg.RunDir, turnCapBytes: cap, cleaner: cfg.Cleaner}
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
func (s *Store) Create(ctx context.Context, p CreateParams) (Conversation, error) {
	id, err := db.ParseUUID("id", p.ID)
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	identityID, err := db.ParseUUID("identity_id", p.IdentityID)
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	row, err := s.q.CreateConversation(ctx, sqlc.CreateConversationParams{
		ID:         id,
		IdentityID: identityID,
		Model:      p.Model,
		Metadata:   p.Metadata,
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation %s: %w", p.ID, err)
	}
	return conversationFromRow(row), nil
}

// Get fetches one conversation, mapping a missing row to ErrConversationNotFound.
func (s *Store) Get(ctx context.Context, conversationID string) (Conversation, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	row, err := s.q.GetConversation(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, fmt.Errorf("get conversation %s: %w", conversationID, ErrConversationNotFound)
		}
		return Conversation{}, fmt.Errorf("get conversation %s: %w", conversationID, err)
	}
	return conversationFromRow(row), nil
}

// List returns conversations ordered by last_active_at DESC. includeArchived adds
// archived rows; deleted rows are always excluded (the query filters status).
func (s *Store) List(ctx context.Context, includeArchived bool) ([]Conversation, error) {
	rows, err := s.q.ListConversations(ctx, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	out := make([]Conversation, 0, len(rows))
	for _, r := range rows {
		out = append(out, conversationFromRow(r))
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
	if err := s.q.UpdateConversationStatus(ctx, sqlc.UpdateConversationStatusParams{ID: id, Status: status}); err != nil {
		return fmt.Errorf("update status %s -> %s: %w", conversationID, status, err)
	}
	return nil
}

// Rename sets the conversation title unconditionally (the CLI `aura chat rename`).
func (s *Store) Rename(ctx context.Context, conversationID, title string) error {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	if err := s.q.RenameConversation(ctx, sqlc.RenameConversationParams{
		ID:    id,
		Title: pgtype.Text{String: title, Valid: true},
	}); err != nil {
		return fmt.Errorf("rename %s: %w", conversationID, err)
	}
	return nil
}

// SetTitleIfNull idempotently sets the title only when it is still NULL (the
// auto-title worker drives this — D-A5-01). A repeat call after the title is set
// is a no-op (the WHERE title IS NULL filters it).
func (s *Store) SetTitleIfNull(ctx context.Context, conversationID, title string) error {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("set title: %w", err)
	}
	if err := s.q.SetConversationTitleIfNull(ctx, sqlc.SetConversationTitleIfNullParams{
		ID:    id,
		Title: pgtype.Text{String: title, Valid: true},
	}); err != nil {
		return fmt.Errorf("set title %s: %w", conversationID, err)
	}
	return nil
}

// CountTurns returns the number of persisted turns for a conversation. The Runner
// uses it for non-fatal bookkeeping such as auto-title eligibility; append sequence
// allocation happens inside AppendTurn's transaction.
func (s *Store) CountTurns(ctx context.Context, conversationID string) (int, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("count turns: %w", err)
	}
	n, err := s.q.CountTurns(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count turns %s: %w", conversationID, err)
	}
	return int(n), nil
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
	rows, err := s.q.ListTurnsBySeq(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load turns %s: %w", conversationID, err)
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

func (s *Store) loadRecentTurns(
	ctx context.Context,
	conversationID string,
	hardCap int,
) ([]Turn, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("load recent turns: %w", err)
	}
	rows, err := s.q.ListRecentTurnsBySeq(
		ctx,
		sqlc.ListRecentTurnsBySeqParams{
			TargetConversationID: id,
			HardCap:              int32(hardCap),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("load recent turns %s: %w", conversationID, err)
	}
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		turn := turnFromRow(sqlc.ListTurnsBySeqRow(row))
		if turn.ContentSidecarPath != "" {
			data, readErr := s.readTurnSidecar(conversationID, turn.Seq)
			if readErr != nil {
				return nil, fmt.Errorf(
					"load recent turns %s seq %d: read sidecar: %w",
					conversationID,
					turn.Seq,
					readErr,
				)
			}
			turn.Content = string(data)
		}
		out = append(out, turn)
	}
	return out, nil
}

// readTurnSidecar reads a spilled turn's content through an os.Root fenced to the
// run dir (LOOP-05 / F-005 / D-08). The DB content_sidecar_path column is a
// did-spill FLAG only: the on-disk path is RECONSTRUCTED from (runDir, convID, seq)
// — identical to what maybeSpill wrote — so a poisoned column value cannot redirect
// the read anywhere, and os.Root refuses to follow a symlink swapped at the
// <seq>.content leaf out of runDir (ASVS V12 path traversal). It mirrors the
// reconstruct-don't-trust fence in agent/tools/read_tool_output.go and adds the
// symlink-leaf neutralization that plain os.Open lacks. Both loadTurns and
// loadBranchTurns call it (DRY). A missing sidecar for a spilled turn stays a HARD
// error the callers wrap — never a silent empty.
func (s *Store) readTurnSidecar(conversationID string, seq int) ([]byte, error) {
	// os.Root.ReadFile takes a RELATIVE path and rejects an absolute one; a relative
	// runDir would resolve the sidecar against the process cwd. Assert an absolute
	// runDir first (mirror read_tool_output.go), then join under the opened root.
	if !filepath.IsAbs(s.runDir) {
		return nil, fmt.Errorf("sidecar runDir %q is not absolute", s.runDir)
	}
	if err := validateID("conversation_id", conversationID); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.runDir)
	if err != nil {
		return nil, fmt.Errorf("open run root %q: %w", s.runDir, err)
	}
	defer func() {
		_ = root.Close()
	}()
	// path.Join (forward-slash, relative), NOT filepath.Join — os.Root paths are
	// always slash-separated relative to the root, never OS-native absolutes.
	rel := path.Join("conversations", conversationID, fmt.Sprintf("%d.content", seq))
	return root.ReadFile(rel)
}

// SearchResult is one FTS hit (the app-side excerpt is the CLI/channel's job).
type SearchResult struct {
	ConversationID string
	Seq            int
	Content        string
	Similarity     float32
}

// SearchConversationTurns wraps the LOCKED cross-slice FTS query (SPEC Req#13 /
// D-A5-03): content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2. The query
// SQL is the contract Telegram /search (Phase 13) reuses byte-for-byte; this
// wrapper only projects pgtype at the boundary. The SQL is never rewritten here.
//
// LOOP-10 / D-10 boundary: spilled turns (content over the cap) store content=NULL
// and are therefore EXCLUDED from this search by construction — `content % $1` never
// matches a NULL. This is a documented+asserted boundary (see maybeSpill and the
// SearchSpill db_integration test), not an oversight: pg_trgm similarity() is
// length-normalized, so a >cap (≥64 KiB) body scores ~0 and would never clear the
// 0.3 threshold even if content were repopulated. The deferred upgrade path is a
// short-preview column (length-compatible with %) at a future migration, never a
// rewrite of this locked query.
func (s *Store) SearchConversationTurns(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.searchTurns(ctx, query, limit, "")
}

// searchTurns is the shared FTS body behind SearchConversationTurns (unscoped) and
// SearchConversationTurnsForIdentity (Phase 36 owner-scoped). The LOCKED sqlc query is
// NEVER rewritten — ownerFilter is applied Go-side alongside the existing deleted-status
// skip (a hit whose conversation is not owned by ownerFilter is dropped so an FTS query
// can never surface another identity's turn content, MUSR-01). ownerFilter == "" keeps
// the pre-Phase-36 unscoped behavior. The per-hit conversation is cached (status + owner
// both read from the one projection) so a repeated conversation costs a single Get.
func (s *Store) searchTurns(ctx context.Context, query string, limit int, ownerFilter string) ([]SearchResult, error) {
	rows, err := s.q.SearchConversationTurns(ctx, sqlc.SearchConversationTurnsParams{
		Similarity: query,
		Limit:      normalizeSearchLimit(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search conversation turns: %w", err)
	}
	out := make([]SearchResult, 0, len(rows))
	convByID := make(map[string]Conversation)
	for _, r := range rows {
		convID := uuid.UUID(r.ConversationID.Bytes).String()
		conv, ok := convByID[convID]
		if !ok {
			loaded, gErr := s.Get(ctx, convID)
			if gErr != nil {
				return nil, fmt.Errorf("search conversation turns: load conversation %s: %w", convID, gErr)
			}
			conv = loaded
			convByID[convID] = loaded
		}
		if conv.Status == StatusDeleted {
			continue
		}
		if ownerFilter != "" && conv.IdentityID != ownerFilter {
			continue
		}
		out = append(out, SearchResult{
			ConversationID: convID,
			Seq:            int(r.Seq),
			Content:        r.Content.String,
			Similarity:     r.Sim,
		})
	}
	return out, nil
}

func normalizeSearchLimit(limit int) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 2147483647 {
		return 2147483647
	}
	return int32(limit)
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
	if err := s.q.DeleteConversation(ctx, id); err != nil {
		return fmt.Errorf("delete conversation %s: %w", conversationID, err)
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
