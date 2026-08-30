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
// each deriving its TTL from its own configured knob. Push creates KindSteer rows;
// PushDelegationResult creates KindDelegationResult rows and requires a fan-out key.
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
//
// Push writes a KindSteer row with a NULL fanout_key. Worker questions use this path too:
// they belong to the live steer rail, never the absent-operator result nudge sweep.
func (s *PostgresStore) Push(conv, source, text string) error {
	return s.push(conv, source, text, KindSteer, "", "")
}

// PushDelegationResult is Push's fan-out-aware counterpart (51-11 Task 3, CONTEXT D-15):
// the SAME guarded INSERT, capacity cap, TTL derivation and sentinel errors, with one
// additional value written -- the fanout_key every row of one swarm_spawn call shares, so
// the absent-operator nudge sweep can claim and deliver the whole fan-out in one message.
// It shares this method's body with Push through the unexported push helper below rather
// than duplicating the validation ladder or the transaction; Push itself keeps its own
// locked, unwidenable (conv, source, text string) error signature untouched (this doc's
// own paragraph above), so a fourth argument was never an option there -- this is a
// second, additive method instead of a fifth Push argument.
func (s *PostgresStore) PushDelegationResult(conv, source, text, fanoutKey string) error {
	if strings.TrimSpace(fanoutKey) == "" {
		return fmt.Errorf("steer: push delegation result: fan-out key is required")
	}
	return s.push(conv, source, text, KindDelegationResult, fanoutKey, "")
}

// PushDelegationResultIdempotent publishes one job-keyed terminal projection.
// Repeating the same conversation/key pair preserves the first body and fan-out.
func (s *PostgresStore) PushDelegationResultIdempotent(conv, source, text, fanoutKey, deliveryKey string) error {
	if strings.TrimSpace(fanoutKey) == "" {
		return fmt.Errorf("steer: push delegation result: fan-out key is required")
	}
	if strings.TrimSpace(deliveryKey) == "" {
		return fmt.Errorf("steer: push delegation result: delivery key is required")
	}
	return s.push(conv, source, text, KindDelegationResult, fanoutKey, deliveryKey)
}

// push is Push and PushDelegationResult's shared body: identical validation ladder,
// guarded INSERT and disambiguation probe. Its callers select the explicit row kind;
// only PushDelegationResult can provide a fan-out key.
func (s *PostgresStore) push(conv, source, text string, kind QueueKind, fanoutKey, deliveryKey string) error {
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
	ttl := s.cfg.SteerTTL
	if kind == KindDelegationResult {
		ttl = s.cfg.DelegationResultTTL
	}
	var expiresAt pgtype.Timestamptz
	if ttl > 0 {
		expiresAt = pgtype.Timestamptz{Time: s.now().Add(ttl), Valid: true}
	}
	var fanoutKeyArg pgtype.Text
	if fanoutKey != "" {
		fanoutKeyArg = pgtype.Text{String: fanoutKey, Valid: true}
	}
	var deliveryKeyArg pgtype.Text
	if deliveryKey != "" {
		deliveryKeyArg = pgtype.Text{String: deliveryKey, Valid: true}
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
			FanoutKey:      fanoutKeyArg,
			DeliveryKey:    deliveryKeyArg,
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
	ID             string
	IdentityID     string
	ConversationID string
	Body           string
	// FanoutKey groups the N rows one swarm_spawn call produced. It is mandatory for
	// every row projected here.
	FanoutKey string
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
			ID:             uuidString(r.ID),
			IdentityID:     uuidString(r.IdentityID),
			ConversationID: r.ConversationID,
			Body:           r.Body,
			FanoutKey:      r.FanoutKey.String,
		})
	}
	return out, nil
}

// MarkFanoutNudged claims every unclaimed row of one (identity, fanout_key) pair in ONE
// statement: two concurrent sweep passes over the SAME
// complete fan-out race for the SAME set of unclaimed rows, and the loser's UPDATE matches
// zero of them (the winner already set nudged_at on all of them), returning an empty
// slice -- never an error, and never a partial claim split across two callers. Each
// returned row carries the body and route captured by that same UPDATE so a sibling
// inserted after the candidate SELECT cannot be marked without being rendered.
func (s *PostgresStore) MarkFanoutNudged(ctx context.Context, identityID, fanoutKey string) ([]UnnudgedDelegationResult, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("steer: mark fanout nudged: store is not configured")
	}
	identity, err := uuid.Parse(identityID)
	if err != nil {
		return nil, fmt.Errorf("steer: mark fanout nudged: invalid identity_id %q: %w", identityID, err)
	}
	var rows []sqlc.MarkFanoutNudgedRow
	err = db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.MarkFanoutNudged(ctx, sqlc.MarkFanoutNudgedParams{
			IdentityID: pgtype.UUID{Bytes: identity, Valid: true},
			FanoutKey:  pgtype.Text{String: fanoutKey, Valid: true},
		})
		return qErr
	})
	if err != nil {
		return nil, fmt.Errorf("steer: mark fanout nudged %s/%s: %w", identityID, fanoutKey, err)
	}
	claimed := make([]UnnudgedDelegationResult, 0, len(rows))
	for _, r := range rows {
		claimed = append(claimed, UnnudgedDelegationResult{
			ID:             uuidString(r.ID),
			IdentityID:     uuidString(r.IdentityID),
			ConversationID: r.ConversationID,
			Body:           r.Body,
			FanoutKey:      r.FanoutKey.String,
		})
	}
	return claimed, nil
}
