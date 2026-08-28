package steer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueKind discriminates aura.steer_queue rows (D-07): one table, rows typed by kind,
// each deriving its TTL from its own configured knob. NOT model-facing and not a Push
// parameter — Push's signature is the shipped, unwidenable (conv, source, text string)
// error contract, so a row's kind is DERIVED from source (source == SourceWorker →
// KindDelegationResult, every other source → KindSteer), never supplied directly.
type QueueKind string

// The two kinds aura.steer_queue accepts. These literals are duplicated in migration
// 0103's CHECK (kind IN ('steer', 'delegation_result')) constraint, so a value drifting
// here fails at INSERT time in Postgres rather than at compile time in Go.
const (
	KindSteer            QueueKind = "steer"
	KindDelegationResult QueueKind = "delegation_result"
)

// PostgresStore is the Postgres-backed steer/delegation-result queue (D-06): the sole
// implementation of the shipped Push/Drain contract since the in-memory Inbox this
// package carried before Phase 51 was deleted, not kept behind a flag. It satisfies
// agent.SteerInbox's Drain(conv string) []Message unchanged, and the Push(conv, source,
// text string) error signature the AG-UI steer route and the Telegram dispatch call
// verbatim.
type PostgresStore struct {
	pool *pgxpool.Pool
	cfg  Config
	now  func() time.Time
}

// NewPostgresStore builds a Postgres-backed steer/delegation-result store from the
// resolved caps and TTLs. A non-positive Max/MaxBytes falls back to the package
// default, mirroring the deleted in-memory Inbox's own New (a cap exists to bound
// storage, not to be silently disabled by an unset/zero env value); a non-positive TTL
// is left as-is (Push's own <=0-disables-expiry contract, the AURA_ASKUSER_PAUSE_TTL_SEC
// precedent — NOT a default substitution).
func NewPostgresStore(pool *pgxpool.Pool, cfg Config) *PostgresStore {
	if cfg.Max <= 0 {
		cfg.Max = defaultMax
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	return &PostgresStore{pool: pool, cfg: cfg, now: time.Now}
}

// Push enqueues text for conv, tagged with source (the channel that produced it — the
// cockpit HTTP route, the Telegram dispatch path, or steer.SourceWorker for a
// delegation's consolidated report). Validates in order — empty/whitespace, oversize,
// per-conversation capacity — returning a distinct sentinel for each so a caller never
// has to string-match. A refused Push never touches the queue.
//
// Push's signature carries neither a context.Context nor an identity: it is called
// verbatim as (conv, source, text string) error by internal/agui/server_run_steer.go and
// internal/channels/telegram/bot_dispatch_steer.go, neither of which may change. The
// owning identity is derived server-side from conv via aura.conversation_owner()
// (migration 0103) inside the SAME guarded INSERT that enforces the capacity cap — see
// that migration's header for why a context.Background()-scoped plain transaction,
// rather than db.WithIdentityTx, is correct here.
func (s *PostgresStore) Push(conv, source, text string) error {
	// The nil-receiver guard MUST run before any field access on s: a
	// *PostgresStore boxed into an interface (telegram.Deps.Steer,
	// agui's steerPusher) can be a non-nil interface wrapping a nil pointer
	// (the classic Go nil-interface trap), and `s.cfg` below would
	// nil-pointer-panic on that value before ever reaching s.pool.
	if s == nil {
		return fmt.Errorf("steer: push: store is not configured")
	}
	if strings.TrimSpace(text) == "" {
		return ErrEmpty
	}
	// Byte semantics, not runes: the cap bounds storage and wire size, and a rune cap
	// would let a multi-byte body exceed the byte bound it exists to enforce.
	// len(string) in Go already IS the encoded byte length.
	if size := len([]byte(text)); size > s.cfg.MaxBytes {
		return ErrTooLarge
	}
	if s.pool == nil {
		return fmt.Errorf("steer: push: store is not configured")
	}
	kind := KindSteer
	ttl := s.cfg.SteerTTL
	if source == SourceWorker {
		kind = KindDelegationResult
		ttl = s.cfg.DelegationResultTTL
	}
	var expiresAt pgtype.Timestamptz
	if ttl > 0 {
		expiresAt = pgtype.Timestamptz{Time: s.now().Add(ttl), Valid: true}
	}

	ctx := context.Background()
	var affected int64
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		affected, qErr = q.PushSteerRow(ctx, sqlc.PushSteerRowParams{
			ConversationID: conv,
			Kind:           string(kind),
			Source:         source,
			Body:           text,
			ExpiresAt:      expiresAt,
			MaxQueue:       int32(s.cfg.Max), //nolint:gosec // Config.Max is an operator-configured cap, always small.
		})
		return qErr
	})
	if err != nil {
		return fmt.Errorf("steer: push %s: %w", conv, err)
	}
	if affected > 0 {
		return nil
	}
	return s.diagnosePushRefusal(ctx, conv)
}

// diagnosePushRefusal runs the cheap owner-only probe PushSteerRow's guarded insert
// leaves for the (overwhelmingly rare) 0-row case: NULL means conv has no resolvable
// owner (a wiring-shaped error — the caller supplied a conversation id nothing owns),
// non-NULL means the (identity, conv) queue was already at Config.Max (ErrQueueFull,
// preserving the deleted in-memory Inbox's exact per-conversation cap semantic).
//
// Rejects a non-uuid conv up front rather than letting Postgres do it: conv flows into
// aura.conversation_owner(text) (migration 0103, cast INSIDE the function body), so a
// malformed conv would otherwise surface as an opaque server-side cast error instead of
// this named one.
func (s *PostgresStore) diagnosePushRefusal(ctx context.Context, conv string) error {
	if _, parseErr := uuid.Parse(conv); parseErr != nil {
		return fmt.Errorf("steer: push: conversation id %q is not a valid uuid: %w", conv, parseErr)
	}
	var owner pgtype.UUID
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		owner, qErr = q.ConversationOwner(ctx, conv)
		return qErr
	})
	if err != nil {
		return fmt.Errorf("steer: push: resolve owner of %s: %w", conv, err)
	}
	if !owner.Valid {
		return fmt.Errorf("steer: push: conversation %s has no resolvable owner", conv)
	}
	return ErrQueueFull
}

// Drain returns every undrained, unexpired row queued for conv, in FIFO (created_at,
// id) order, marking them drained in the same statement — a second Drain on the same
// conv returns empty. A row past its own expires_at is excluded even before the sweep
// catches up (lazy expiry on read).
//
// Drain's signature carries no context.Context either (agent.SteerInbox's
// Drain(conv string) []Message, unchanged) and no error return, so a malformed conv or a
// transient DB failure degrades to an empty slice with a WARN log — the same
// best-effort posture conversations.ScanOrphans already uses for a background
// operation with no channel to report failure through. The agent's drainSteer/
// deliverLeftoverSteer already treat an empty drain as "nothing to deliver", never as
// fatal.
func (s *PostgresStore) Drain(conv string) []Message {
	if s == nil || s.pool == nil {
		return nil
	}
	if _, parseErr := uuid.Parse(conv); parseErr != nil {
		slog.Warn("steer: drain: conversation id is not a valid uuid", "conv", conv, "err", parseErr)
		return nil
	}
	ctx := context.Background()
	var rows []sqlc.DrainSteerRowsRow
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.DrainSteerRows(ctx, conv)
		return qErr
	})
	if err != nil {
		slog.Warn("steer: drain failed", "conv", conv, "err", err)
		return nil
	}
	msgs := make([]Message, 0, len(rows))
	for _, r := range rows {
		msgs = append(msgs, Message{
			ID:      uuidString(r.ID),
			Source:  r.Source,
			Text:    r.Body,
			Arrived: r.CreatedAt.Time,
		})
	}
	return msgs
}

func uuidString(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	return uuid.UUID(v.Bytes).String()
}

// UnnudgedDelegationResult is one aura.steer_queue delegation_result row past
// the nudge grace window that the operator never drained (plan 51-10's
// absent-operator leg). Declared here rather than reusing swarm.UndrainedResult:
// internal/steer must not import internal/swarm, which already imports
// internal/steer for steer.SourceWorker -- the reverse edge would cycle. The
// cmd/aura composition root is the one place that translates between the two
// packages' independently-declared, structurally-identical row types.
type UnnudgedDelegationResult struct {
	ID         string
	IdentityID string
	Body       string
}

// ListUnnudgedDelegationResults returns delegation_result rows older than
// cutoff that the operator never drained (drained_at IS NULL), not expired,
// not already nudged. Unscoped by identity, exactly like Sweeper.ExpireDue's
// own ListDueSteerRows call (aura.steer_queue carries no RLS, migration
// 0103): a system-wide sweep, each row carrying its own identity_id.
func (s *PostgresStore) ListUnnudgedDelegationResults(ctx context.Context, cutoff time.Time, limit int) ([]UnnudgedDelegationResult, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("steer: list unnudged delegation results: store is not configured")
	}
	var rows []sqlc.AuraSteerQueue
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.ListUnnudgedDelegationResults(ctx, sqlc.ListUnnudgedDelegationResultsParams{
			Cutoff:   pgtype.Timestamptz{Time: cutoff, Valid: true},
			RowLimit: int32(limit), //nolint:gosec // an operator-configured sweep batch size, always small.
		})
		return qErr
	})
	if err != nil {
		return nil, fmt.Errorf("steer: list unnudged delegation results: %w", err)
	}
	out := make([]UnnudgedDelegationResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnnudgedDelegationResult{
			ID:         uuidString(r.ID),
			IdentityID: uuidString(r.IdentityID),
			Body:       r.Body,
		})
	}
	return out, nil
}

// MarkSteerRowNudged is the claim-before-push idempotency key (SWARM-09
// edge): a conditional UPDATE ... WHERE nudged_at IS NULL, so two CONCURRENT
// sweep passes over the SAME row race for exactly one winner (RowsAffected==1
// for the winner, 0 for the loser) -- the same conditional-update-as-claim
// idiom DrainSteerRows' FOR UPDATE already establishes for the operator-
// present path, generalized here to a plain conditional UPDATE since there is
// no multi-row candidate SET to lock ahead of time.
func (s *PostgresStore) MarkSteerRowNudged(ctx context.Context, id, identityID string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, fmt.Errorf("steer: mark steer row nudged: store is not configured")
	}
	rowID, err := uuid.Parse(id)
	if err != nil {
		return false, fmt.Errorf("steer: mark steer row nudged: invalid id %q: %w", id, err)
	}
	identity, err := uuid.Parse(identityID)
	if err != nil {
		return false, fmt.Errorf("steer: mark steer row nudged: invalid identity_id %q: %w", identityID, err)
	}
	var affected int64
	err = db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		affected, qErr = q.MarkSteerRowNudged(ctx, sqlc.MarkSteerRowNudgedParams{
			ID:         pgtype.UUID{Bytes: rowID, Valid: true},
			IdentityID: pgtype.UUID{Bytes: identity, Valid: true},
		})
		return qErr
	})
	if err != nil {
		return false, fmt.Errorf("steer: mark steer row nudged %s: %w", id, err)
	}
	return affected > 0, nil
}
