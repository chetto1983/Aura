package runner

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkerPauseQueueResolver is the Tx-bound half of resolving a parked
// aura.ingestion_jobs row to its terminal state, declared here (the consumer) so
// internal/runner never imports internal/documents: cmd/aura adapts
// *documents.PostgresIngestionJobStore.ResolveIngestionJobAwaitingInputTx onto it.
// It opens NO transaction of its own -- q is the caller's, so the resolution commits
// or rolls back together with the pause claim and the trace turn. A zero rows
// result means the row is no longer awaiting_input.
type WorkerPauseQueueResolver interface {
	ResolveAwaitingInputTx(ctx context.Context, q *sqlc.Queries, identityID, jobID, errorMessage string) (int64, error)
}

// PoolWorkerPauseExpirer is the production WorkerPauseExpirer: it owns the shared
// *pgxpool.Pool and the CONCRETE askuser/conversations Stores, so ExpireWorkerPause
// runs ONE db.WithIdentityTx composing three tx-accepting halves -- the fenced pause
// claim (askuser.MarkResumedFencedTx, D-12), the assistant trace turn
// (conversations.AppendTurnTx under the conversation row-lock, the SAME seq allocator
// PoolResumeCommitter uses), and the parked queue row's resolution. D-08: if the
// trace write fails, the pause is NOT marked expired; if the row resolution fails,
// neither is. Mirrors PoolResumeCommitter's cross-store tx composition exactly --
// never a second concurrency story. The identity comes from the expiry, not the
// context: the sweep runs on a daemon loop with no caller identity of its own.
type PoolWorkerPauseExpirer struct {
	pool  *pgxpool.Pool
	conv  *conversations.Store
	pause *askuser.Store
	queue WorkerPauseQueueResolver
}

// NewPoolWorkerPauseExpirer builds the atomic expirer over the shared pool + concrete
// stores. The composition root (cmd/aura/serve_delegation.go) hands it to the sweep.
func NewPoolWorkerPauseExpirer(pool *pgxpool.Pool, conv *conversations.Store, pause *askuser.Store, queue WorkerPauseQueueResolver) *PoolWorkerPauseExpirer {
	return &PoolWorkerPauseExpirer{pool: pool, conv: conv, pause: pause, queue: queue}
}

// ExpireWorkerPause claims the pause (fenced), appends its trace, and resolves its
// parked row, in one tx. A failed claim (rows==0 -> askuser.ErrPauseNotFound: the
// operator answered first, or the row vanished) returns before any write; a failed
// trace or resolution rolls the claim back, leaving resumed_at IS NULL so the next
// pass retries. A resolution matching zero rows while the claim succeeded is a hard
// error (never silently skipped): a pause claimed exactly once whose row is no longer
// parked is a state Task 1's one-transaction park makes impossible.
func (p *PoolWorkerPauseExpirer) ExpireWorkerPause(ctx context.Context, e WorkerPauseExpiry) error {
	if p == nil || p.pool == nil || p.conv == nil || p.pause == nil || p.queue == nil {
		return fmt.Errorf("worker pause expirer is not configured")
	}
	return db.WithIdentityTx(ctx, p.pool, e.IdentityID, func(q *sqlc.Queries) error {
		if err := p.pause.MarkResumedFencedTx(ctx, q, e.Claim.Token, e.Claim.Answer, e.Claim.ExpectActionID); err != nil {
			return err
		}
		seq, err := allocateResumeTurnSeq(ctx, q, e.Claim.Turn.ConversationID)
		if err != nil {
			return err
		}
		turn := e.Claim.Turn
		turn.Seq = seq
		if err := p.conv.AppendTurnTx(ctx, q, turn); err != nil {
			return fmt.Errorf("worker pause trace: %w", err)
		}
		n, err := p.queue.ResolveAwaitingInputTx(ctx, q, e.IdentityID, e.JobID, e.ErrorMessage)
		if err != nil {
			return fmt.Errorf("worker pause queue row %s: %w", e.JobID, err)
		}
		if n == 0 {
			return fmt.Errorf("worker pause queue row %s: no longer awaiting_input while its pause %s was still pending", e.JobID, e.Claim.Token)
		}
		return nil
	})
}

var _ WorkerPauseExpirer = (*PoolWorkerPauseExpirer)(nil)
