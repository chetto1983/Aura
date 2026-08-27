package steer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errRowAlreadyExpired signals expireOne's own idempotency gate fired (a prior sweep
// pass, or a concurrent one, already marked this row): NOT an error the sweep reports,
// mirroring approval_expiry.go's errors.Is(err, askuser.ErrPauseNotFound) { continue }.
var errRowAlreadyExpired = errors.New("steer: row already expired")

// Sweeper expires due aura.steer_queue rows (D-07/D-08): one sweep, two TTL knobs
// already baked into each row's own expires_at at Push time (steer.PostgresStore.Push),
// one readable conversation trace per expired row, written in the SAME transaction that
// marks it expired. Follows internal/runner/approval_expiry.go's shape exactly: list
// due candidates, loop, commit each outcome, count, never drive a model turn.
type Sweeper struct {
	pool *pgxpool.Pool
	conv *conversations.Store
}

// NewSweeper builds a Sweeper over the shared pool and the SAME *conversations.Store
// the runner's own resume committer writes through — no second conversation-write path.
func NewSweeper(pool *pgxpool.Pool, conv *conversations.Store) *Sweeper {
	return &Sweeper{pool: pool, conv: conv}
}

// ExpireDue selects up to limit rows whose expires_at is <= now (a row with
// expires_at IS NULL is never selected — it never expires), and for each, inside ONE
// per-row transaction: marks it expired_at/expiry_reason and appends a readable trace
// naming the row's kind and source to its conversation. A row whose trace append fails
// is NOT marked expired (the transaction rolls back) — its error joins the returned
// error rather than aborting the whole pass, so one bad row cannot stall every other
// due row's expiry. A row already expired by a prior (or concurrent) pass is skipped,
// not failed: the sweep is idempotent.
//
// ListDueSteerRows runs unscoped across every identity (aura.steer_queue carries no
// RLS, migration 0103) — the sweep is a system-wide background job, not a per-identity
// request, so unlike ExpirePendingApprovals's own caller (cmd/aura/approval_expiry.go's
// per-identity enumeration loop) this needs no external identity list: each returned
// row already carries its own identity_id, and expireOne opens ONE identity-scoped
// transaction from it.
func (s *Sweeper) ExpireDue(ctx context.Context, now time.Time, limit int) (int, error) {
	if s == nil || s.pool == nil || s.conv == nil {
		return 0, fmt.Errorf("steer: expire due: sweeper is not configured")
	}
	var due []sqlc.AuraSteerQueue
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var qErr error
		due, qErr = q.ListDueSteerRows(ctx, sqlc.ListDueSteerRowsParams{
			Cutoff:   pgtype.Timestamptz{Time: now, Valid: true},
			RowLimit: int32(limit), //nolint:gosec // an operator-configured sweep batch size, always small.
		})
		return qErr
	})
	if err != nil {
		return 0, fmt.Errorf("steer: list due rows: %w", err)
	}
	expired := 0
	var errs []error
	for _, row := range due {
		if err := s.expireOne(ctx, row); err != nil {
			if errors.Is(err, errRowAlreadyExpired) {
				continue
			}
			errs = append(errs, fmt.Errorf("expire steer row %s: %w", uuidString(row.ID), err))
			continue
		}
		expired++
	}
	return expired, errors.Join(errs...)
}

// expireOne marks ONE row expired and appends its conversation trace atomically. The
// cutoff/TTL is never recomputed here: it was already baked into row.ExpiresAt per its
// OWN kind at Push time (D-07's "one sweep, two knobs" means the knobs are consulted
// once, at write time, not re-derived on every sweep tick).
func (s *Sweeper) expireOne(ctx context.Context, row sqlc.AuraSteerQueue) error {
	identityID := uuidString(row.IdentityID)
	if identityID == "" {
		return fmt.Errorf("row carries no identity_id")
	}
	reason := expiryReasonFor(row.Kind)
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		return markAndTrace(
			func() (int64, error) {
				return q.MarkSteerRowExpired(ctx, sqlc.MarkSteerRowExpiredParams{
					ExpiryReason: pgtype.Text{String: reason, Valid: true},
					ID:           row.ID,
					IdentityID:   row.IdentityID,
				})
			},
			func() error {
				seq, err := allocateSweepTurnSeq(ctx, q, row.ConversationID)
				if err != nil {
					return fmt.Errorf("allocate trace turn seq: %w", err)
				}
				return s.conv.AppendTurnTx(ctx, q, conversations.AppendTurnParams{
					ConversationID: row.ConversationID,
					Seq:            seq,
					Role:           llm.RoleAssistant,
					Content:        expiryTraceFor(row),
				})
			},
		)
	})
}

// markAndTrace is D-08's own load-bearing sequencing, pulled out of expireOne's
// db.WithIdentityTx closure as a pure, daemon-free-testable unit: mark THEN trace, and
// a trace failure propagates as a non-nil error so the surrounding transaction (already
// covered by internal/db's own WithTx rollback tests) rolls back the mark too — "the
// same transaction that marks it expired writes the trace" is enforced by never letting
// a trace failure look like anything other than the WHOLE operation's failure. mark
// returning n==0 (already expired by a prior/concurrent pass) short-circuits BEFORE
// appendTrace ever runs, so an idempotent second pass never writes a second trace.
func markAndTrace(mark func() (int64, error), appendTrace func() error) error {
	n, err := mark()
	if err != nil {
		return fmt.Errorf("mark expired: %w", err)
	}
	if n == 0 {
		return errRowAlreadyExpired
	}
	return appendTrace()
}

// expiryReasonFor is the short, machine-stable aura.steer_queue.expiry_reason value —
// distinct from expiryTraceFor's human-readable conversation trace.
func expiryReasonFor(kind string) string {
	if kind == string(KindDelegationResult) {
		return "delegation_result_ttl_expired"
	}
	return "steer_ttl_expired"
}

// expiryTraceFor is D-08's readable conversation trace, naming the row's kind and
// source so the agent can truthfully report what did not happen — a delegation-result
// trace states which worker's report was never delivered; a steer trace states an
// operator redirect went unheard.
func expiryTraceFor(row sqlc.AuraSteerQueue) string {
	if row.Kind == string(KindDelegationResult) {
		return fmt.Sprintf(
			"A delegated worker's report (source: %s) expired before it reached this conversation and was discarded undelivered.",
			row.Source,
		)
	}
	return fmt.Sprintf(
		"An operator steer (source: %s) expired before it reached this conversation and was discarded undelivered.",
		row.Source,
	)
}

// allocateSweepTurnSeq reserves the next per-conversation turn seq INSIDE the caller's
// tx by row-locking the parent conversation then reading MAX(seq)+1 — the same two
// sqlc queries internal/runner/resume_committer.go's allocateResumeTurnSeq wraps,
// called directly off the shared sqlc.Queries surface (D-02's "one sqlc.New(tx) exposes
// every query needed" reasoning): re-using the query layer, not a cross-package export
// minted only for this file, is the minimal seam.
func allocateSweepTurnSeq(ctx context.Context, q *sqlc.Queries, conversationID string) (int, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return 0, fmt.Errorf("allocate sweep turn seq: invalid conversation_id %q: %w", conversationID, err)
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	if _, err := q.LockConversationForTurnAppend(ctx, pgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("allocate sweep turn seq %s: %w", conversationID, conversations.ErrConversationNotFound)
		}
		return 0, fmt.Errorf("lock conversation %s for sweep turn append: %w", conversationID, err)
	}
	seq, err := q.NextConversationTurnSeq(ctx, pgID)
	if err != nil {
		return 0, fmt.Errorf("next sweep turn seq %s: %w", conversationID, err)
	}
	return int(seq), nil
}
