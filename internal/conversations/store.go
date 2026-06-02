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
)

// ErrConversationNotFound is a missing conversation lookup — a sentinel so callers
// classify the failure without string matching.
var ErrConversationNotFound = errors.New("conversation not found")

// Store wraps a pgx pool and the generated Queries — the canonical shape from
// internal/identity. Non-tx reads/writes use s.q; the atomic per-turn write
// (AppendTurn) and delete-then-rm wrap db.WithTx / pool ops. runDir is the
// $AURA_RUN_DIR root the sidecar spill writes under; turnCapBytes is
// AURA_CONVERSATION_TURN_CAP_BYTES (content over this spills to a sidecar file).
type Store struct {
	pool         *pgxpool.Pool
	q            *sqlc.Queries
	runDir       string
	turnCapBytes int
}

// Config carries the Store's filesystem + spill knobs (from config.Config).
type Config struct {
	RunDir       string // $AURA_RUN_DIR — sidecar root
	TurnCapBytes int    // AURA_CONVERSATION_TURN_CAP_BYTES (65536)
}

// New builds a Store over an open pool. A zero TurnCapBytes falls back to the
// SPEC default so a misconfigured caller never disables spillover entirely.
func New(pool *pgxpool.Pool, cfg Config) *Store {
	cap := cfg.TurnCapBytes
	if cap <= 0 {
		cap = 65536
	}
	return &Store{pool: pool, q: sqlc.New(pool), runDir: cfg.RunDir, turnCapBytes: cap}
}

// Conversation is the domain projection of aura.conversations — plain Go types at
// the package boundary instead of the sqlc pgtype wrappers. Title is the rendered
// title (NULL renders "(untitled <created_at>)" via DisplayTitle).
type Conversation struct {
	ID                string
	Title             string // empty when the DB title is NULL
	TitleSet          bool   // false when the DB title is NULL
	IdentityID        string
	Status            string
	Model             string
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCachedTokens int64
	TotalCostUSD      float64
	CreatedAt         string // RFC3339; used by DisplayTitle for the untitled fallback
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
	id, err := parseUUID("id", p.ID)
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	identityID, err := parseUUID("identity_id", p.IdentityID)
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
	id, err := parseUUID("id", conversationID)
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
	id, err := parseUUID("id", conversationID)
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
	id, err := parseUUID("id", conversationID)
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
	id, err := parseUUID("id", conversationID)
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
// reads it to decide the next seq and whether to fire the auto-title (seq >= 3).
func (s *Store) CountTurns(ctx context.Context, conversationID string) (int, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("count turns: %w", err)
	}
	n, err := s.q.CountTurns(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count turns %s: %w", conversationID, err)
	}
	return int(n), nil
}

// AppendTurnParams carries one new turn plus its token/cost delta. Cost is the
// USD figure to fold into the running total_cost_usd aggregate (RESEARCH OQ4 —
// aggregated in SQL inside the same tx).
type AppendTurnParams struct {
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ToolCallID     string
	ToolCalls      []byte
	InputTokens    int
	OutputTokens   int
	CachedTokens   int
	CostUSD        float64
}

// AppendTurn writes one turn AND folds its token/cost delta into the conversation
// aggregates in a SINGLE db.WithTx transaction (SC-2): a failure between the turn
// INSERT and the aggregates UPDATE rolls the whole thing back, leaving no partial
// turn. Content over turnCapBytes spills to a sidecar file BEFORE the tx (the file
// write is not part of the DB atomicity; a later boot scan reconciles an orphaned
// file if the tx rolls back). The row then stores content=NULL + content_sidecar_path.
func (s *Store) AppendTurn(ctx context.Context, p AppendTurnParams) error {
	turn, agg, err := s.appendTurnWrites(p)
	if err != nil {
		return err
	}

	if err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		return insertTurnAndAggregates(ctx, q, turn, agg)
	}); err != nil {
		return fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return nil
}

// AppendAssistantTurnWithCacheMetric writes a terminal assistant turn, folds its
// usage into the conversation aggregate, and appends the matching cache_metrics row
// in one database transaction. The Runner uses this for final assistant Events so a
// metric failure cannot leave the conversation with an answer the caller never
// receives.
func (s *Store) AppendAssistantTurnWithCacheMetric(ctx context.Context, p AppendTurnParams, metric sqlc.InsertCacheMetricParams) error {
	turn, agg, err := s.appendTurnWrites(p)
	if err != nil {
		return err
	}

	if err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		if err := insertTurnAndAggregates(ctx, q, turn, agg); err != nil {
			return err
		}
		if err := q.InsertCacheMetric(ctx, metric); err != nil {
			return fmt.Errorf("insert cache metric: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("append assistant turn with cache metric %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return nil
}

func (s *Store) appendTurnWrites(p AppendTurnParams) (sqlc.InsertConversationTurnParams, sqlc.UpdateConversationAggregatesParams, error) {
	convID, err := parseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn: %w", err)
	}
	content, sidecarPath, err := s.maybeSpill(p.ConversationID, p.Seq, p.Content)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	cost, err := numericFromFloat(p.CostUSD)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}

	turn := sqlc.InsertConversationTurnParams{
		ConversationID:     convID,
		Seq:                int32(p.Seq),
		Role:               p.Role,
		Content:            content,
		ContentSidecarPath: sidecarPath,
		ToolCallID:         optionalText(p.ToolCallID),
		ToolCalls:          p.ToolCalls,
		InputTokens:        int32(p.InputTokens),
		OutputTokens:       int32(p.OutputTokens),
		CachedTokens:       int32(p.CachedTokens),
	}
	agg := sqlc.UpdateConversationAggregatesParams{
		InputTokens:  int64(p.InputTokens),
		OutputTokens: int64(p.OutputTokens),
		CachedTokens: int64(p.CachedTokens),
		CostUsd:      cost,
		ID:           convID,
	}

	return turn, agg, nil
}

func insertTurnAndAggregates(ctx context.Context, q *sqlc.Queries, turn sqlc.InsertConversationTurnParams, agg sqlc.UpdateConversationAggregatesParams) error {
	if err := q.InsertConversationTurn(ctx, turn); err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}
	if err := q.UpdateConversationAggregates(ctx, agg); err != nil {
		return fmt.Errorf("update aggregates: %w", err)
	}
	return nil
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
	return out, nil
}

// loadTurns fetches the ordered Turn projections, rehydrating sidecar-spilled
// content from disk. Shared by LoadHistory and the context ladder.
func (s *Store) loadTurns(ctx context.Context, conversationID string) ([]Turn, error) {
	id, err := parseUUID("conversation_id", conversationID)
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
			data, rerr := os.ReadFile(t.ContentSidecarPath)
			if rerr != nil {
				return nil, fmt.Errorf("load turns %s seq %d: read sidecar %q: %w",
					conversationID, t.Seq, t.ContentSidecarPath, rerr)
			}
			t.Content = string(data)
		}
		out = append(out, t)
	}
	return out, nil
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
func (s *Store) SearchConversationTurns(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	rows, err := s.q.SearchConversationTurns(ctx, sqlc.SearchConversationTurnsParams{
		Similarity: query,
		Limit:      normalizeSearchLimit(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search conversation turns: %w", err)
	}
	out := make([]SearchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, SearchResult{
			ConversationID: uuid.UUID(r.ConversationID.Bytes).String(),
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
// via FK ON DELETE CASCADE), then os.RemoveAll the sidecar dir AFTER the DB delete
// commits. A filesystem rm failure is a WARN-level degradation (returned as an
// error the caller may log), NOT a rolled-back delete — the boot orphan scan
// reconciles a leftover dir at next start (SPEC Req#12).
func (s *Store) Delete(ctx context.Context, conversationID string) error {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if err := s.q.DeleteConversation(ctx, id); err != nil {
		return fmt.Errorf("delete conversation %s: %w", conversationID, err)
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
