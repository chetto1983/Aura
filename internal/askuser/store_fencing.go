package askuser

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// store_fencing.go holds the D-12 (fencing id) / D-13 (level identity) additions from
// 51-06a (SWARM-06 guard rails, checkpoint decision `new-columns`, migration 0106) — kept
// out of store.go so that file stays under the 600-LOC refactor-on-touch ceiling.
//
// SCOPE BOUNDARY: this plan makes a pause fenceable, attributable and lazily-expiring.
// It does NOT continue a paused worker — persisting continuation state, observing an
// answered pause, and injecting the answer are plan 51-06b (wave 4).

// WorkerID answers "which worker owns this pause" (D-13), preferring the host-written
// OwningWorkerID (a background per-worker pause) over the model-relayed
// ProxiedFromChildID (a synchronous parent-relayed pause). This is the ONE accessor
// callers must use instead of reading either field directly: the migration 0106 CHECK
// constraint (paused_states_worker_attribution_exclusive) guarantees at most one is ever
// set on a row, so there is no real conflict to arbitrate — only a single place that
// answers the question, so a model-supplied relay id can never be read as if it were the
// host-authoritative one (T-51-47).
func (p Pending) WorkerID() string {
	if p.OwningWorkerID != "" {
		return p.OwningWorkerID
	}
	return p.ProxiedFromChildID
}

// FencedResumeAnswer pairs one pause's AM-02 answer with its D-12 fencing id for
// MarkResumedBatchTx. ExpectActionID empty means the caller supplies no fence: the
// shipped `pending_action_id IS NULL` half of MarkPausedStateResumedFenced's predicate
// still governs, so an unfenced/legacy pause resumes exactly as it did before migration
// 0106 — the fence is additive, never a new precondition on the shipped path.
type FencedResumeAnswer struct {
	Answer         ResumeAnswer
	ExpectActionID string
}

// markPausedStateResumedFencedTx is the ONE call site for D-12's fenced conditional
// UPDATE (MarkPausedStateResumedFenced). MarkResumedFencedTx (single claim) and
// MarkResumedBatchTx's per-token loop (store.go) both route through it, so the fencing
// predicate has exactly one Go-side implementation to drift, not two independently
// written ones (approval-resume-defects, folded constraint).
func markPausedStateResumedFencedTx(ctx context.Context, q *sqlc.Queries, id pgtype.UUID, answer []byte, expectActionID string) (int64, error) {
	return q.MarkPausedStateResumedFenced(ctx, sqlc.MarkPausedStateResumedFencedParams{
		Token:          id,
		ResumedAnswer:  answer,
		ExpectActionID: pgtype.Text{String: expectActionID, Valid: expectActionID != ""},
	})
}

// MarkResumedFencedTx resolves ONE pause with the AM-02 answer AND its D-12 fencing id,
// using the caller-supplied Queries (bound to the caller's transaction — it opens NO
// transaction of its own). It is the single-claim sibling of MarkResumedBatchTx's
// per-token step: the runner's PoolResumeCommitter.CommitResume calls this instead of the
// unfenced MarkResumedTx so a ResumeClaim's ExpectActionID is honored on both the single
// and batch resume paths (interfaces.go's ResumeClaim is the SAME struct for both).
func (s *Store) MarkResumedFencedTx(ctx context.Context, q *sqlc.Queries, token string, ans ResumeAnswer, expectActionID string) error {
	id, err := db.ParseUUID("token", token)
	if err != nil {
		return fmt.Errorf("mark resumed fenced: %w", err)
	}
	answer, err := encodeAnswer(ans)
	if err != nil {
		return fmt.Errorf("mark resumed fenced %s: %w", token, err)
	}
	n, err := markPausedStateResumedFencedTx(ctx, q, id, answer, expectActionID)
	if err != nil {
		return fmt.Errorf("mark resumed fenced %s: %w", token, err)
	}
	if n == 0 {
		return fmt.Errorf("mark resumed fenced %s: %w", token, ErrPauseNotFound)
	}
	return nil
}

// MarkResumedFenced resolves ONE pause with the AM-02 answer AND its D-12 fencing id, in
// a transaction scoped to the identity on ctx. A thin wrapper over MarkResumedFencedTx,
// mirroring the MarkResumed/MarkResumedTx pairing.
func (s *Store) MarkResumedFenced(ctx context.Context, token string, ans ResumeAnswer, expectActionID string) error {
	return s.scoped(ctx, func(q *sqlc.Queries) error {
		return s.MarkResumedFencedTx(ctx, q, token, ans, expectActionID)
	})
}

// NewWithPauseTTL builds a Store whose GetByToken additionally applies D-12/D-13's lazy
// expiry (mirroring LibreChat ApprovalLifecycle.peek): a still-pending row whose
// created_at is at least ttlSec old reads as ErrPauseNotFound, before the sweeper (plan
// 51-06b) ever claims it. ttlSec<=0 disables the check — the same value as New(pool),
// which every production call site (cmd/aura/chat_boot.go, cmd/aura/paused_states.go)
// still uses today, so this constructor is additive and inert until a caller opts in.
func NewWithPauseTTL(pool *pgxpool.Pool, ttlSec int) *Store {
	return &Store{pool: pool, pauseTTLSec: ttlSec}
}

// pauseExpired is the Store-bound half of the D-12/D-13 lazy-expiry check: it supplies
// the configured TTL and the real clock to the pure comparison below.
func (s *Store) pauseExpired(createdAt pgtype.Timestamptz) bool {
	return pauseExpired(createdAt, s.pauseTTLSec, time.Now())
}

// pauseExpired is the pure D-12/D-13 lazy-expiry comparison (daemon-free unit-testable,
// no Postgres required): disabled (false) when TTL tracking is off (ttlSec<=0) or the row
// carries no created_at, otherwise true once now has moved at least ttlSec seconds past
// createdAt.
func pauseExpired(createdAt pgtype.Timestamptz, ttlSec int, now time.Time) bool {
	if ttlSec <= 0 || !createdAt.Valid {
		return false
	}
	return now.Sub(createdAt.Time) >= time.Duration(ttlSec)*time.Second
}
