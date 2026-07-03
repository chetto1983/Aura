package runner

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolResumeCommitter is the production ResumeCommitter: it owns the shared *pgxpool.Pool
// and the CONCRETE askuser/conversations Stores, so each method runs ONE db.WithTx that
// composes their 34-05 tx-accepting methods (D-03). Because aura.paused_states and
// aura.conversation_turns live in the SAME generated sqlc package, a single sqlc.New(tx)
// exposes every query needed to claim a pause AND append its answer turn atomically
// (D-02) — no ledger, no second durability mechanism. It cannot be unit-faked (db.WithTx
// calls pool.Begin on a concrete pool), so its atomicity is proven by the db_integration
// tier. This tx carries NO cleanupSidecarOnTxError composition: AppendTurnTx ENFORCES the
// A3 invariant (resume/pause turns < turnCapBytes) by rejecting a would-be spill with
// conversations.ErrContentSpillUnsupported before any sidecar is written, so an over-cap
// answer rolls the whole tx back cleanly instead of orphaning a file (WR-01).
type PoolResumeCommitter struct {
	pool  *pgxpool.Pool
	conv  *conversations.Store
	pause *askuser.Store
}

// NewPoolResumeCommitter builds the atomic committer over the shared pool + concrete
// stores. The composition root (cmd/aura/chat.go) injects it into runner.Deps.
func NewPoolResumeCommitter(pool *pgxpool.Pool, conv *conversations.Store, pause *askuser.Store) *PoolResumeCommitter {
	return &PoolResumeCommitter{pool: pool, conv: conv, pause: pause}
}

// CommitResume claims the pause then appends its answer turn in one tx. A failed claim
// (rows==0 → ErrPauseNotFound) returns before any append; a failed append rolls the
// claim back, leaving resumed_at IS NULL so the user can retry (D-06/LOOP-03).
func (p *PoolResumeCommitter) CommitResume(ctx context.Context, claim ResumeClaim) error {
	return db.WithTx(ctx, p.pool, func(q *sqlc.Queries) error {
		if err := p.pause.MarkResumedTx(ctx, q, claim.Token, claim.Answer); err != nil {
			return err
		}
		return p.appendAnswerTx(ctx, q, claim.Turn)
	})
}

// CommitResumeBatch claims ALL pauses (MarkResumedBatchTx claims in sorted-token order →
// deadlock-free under concurrent overlapping batches) then appends every answer turn, in
// one tx. Any rows==0 claim rolls the whole tx back → exactly one answer per pause, no
// orphan RoleTool turns (D-04/LOOP-02). Appends run in sorted-token order too so the
// reserved seqs are deterministic.
func (p *PoolResumeCommitter) CommitResumeBatch(ctx context.Context, claims []ResumeClaim) error {
	if len(claims) == 0 {
		return nil
	}
	answers := make(map[string]askuser.ResumeAnswer, len(claims))
	for _, c := range claims {
		answers[c.Token] = c.Answer
	}
	ordered := append([]ResumeClaim(nil), claims...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Token < ordered[j].Token })
	return db.WithTx(ctx, p.pool, func(q *sqlc.Queries) error {
		if err := p.pause.MarkResumedBatchTx(ctx, q, answers); err != nil {
			return err
		}
		for _, c := range ordered {
			if err := p.appendAnswerTx(ctx, q, c.Turn); err != nil {
				return err
			}
		}
		return nil
	})
}

// CommitPause writes the assistant ask_user tool_call turn + all N paused_states rows in
// one tx (D-05/LOOP-04): the assistant turn goes in FIRST so a pause is never visible
// without its wire-valid preceding history. A tx failure leaves NEITHER the turn nor the
// pauses.
func (p *PoolResumeCommitter) CommitPause(ctx context.Context, assistantTurn conversations.AppendTurnParams, pauses []askuser.InsertParams) error {
	if len(pauses) == 0 {
		return nil
	}
	return db.WithTx(ctx, p.pool, func(q *sqlc.Queries) error {
		if err := p.appendAnswerTx(ctx, q, assistantTurn); err != nil {
			return err
		}
		for _, ins := range pauses {
			if err := p.pause.InsertTx(ctx, q, ins); err != nil {
				return err
			}
		}
		return nil
	})
}

// appendAnswerTx reserves the next turn seq under the conversation row-lock and appends
// the turn via the 34-05 AppendTurnTx, all on the caller's tx. AppendTurnTx requires
// Seq>0 and never allocates one, so the seq must be reserved here.
func (p *PoolResumeCommitter) appendAnswerTx(ctx context.Context, q *sqlc.Queries, turn conversations.AppendTurnParams) error {
	seq, err := allocateResumeTurnSeq(ctx, q, turn.ConversationID)
	if err != nil {
		return err
	}
	turn.Seq = seq
	return p.conv.AppendTurnTx(ctx, q, turn)
}

// allocateResumeTurnSeq reserves the next per-conversation turn seq INSIDE the caller's
// tx by row-locking the parent conversation then reading MAX(seq)+1. It re-uses the SAME
// two sqlc queries conversations.Store.allocateTurnSeq wraps, called directly off the
// shared sqlc.Queries surface (D-02): that allocator is unexported and this plan is
// scoped out of the conversations package, so re-using the query layer — not a
// cross-package export minted only for the runner — is the minimal seam. The row-lock is
// held to tx end, so repeated allocations within one tx return sequential seqs.
func allocateResumeTurnSeq(ctx context.Context, q *sqlc.Queries, conversationID string) (int, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return 0, fmt.Errorf("allocate resume turn seq: invalid conversation_id %q: %w", conversationID, err)
	}
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	if _, err := q.LockConversationForTurnAppend(ctx, pgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("allocate resume turn seq %s: %w", conversationID, conversations.ErrConversationNotFound)
		}
		return 0, fmt.Errorf("lock conversation %s for resume turn append: %w", conversationID, err)
	}
	seq, err := q.NextConversationTurnSeq(ctx, pgID)
	if err != nil {
		return 0, fmt.Errorf("next resume turn seq %s: %w", conversationID, err)
	}
	return int(seq), nil
}

// splitResumeCommitter is the pool-less compatibility fallback (NON-atomic). It composes
// only the narrow Conv/Pause interfaces so pool-less contexts (cache_audit, unit tests)
// keep compiling and running with no injected pool. It preserves the pre-34-06 order —
// claim-then-append for a resume, claim-all-then-append-all for a batch (fixing the
// inject-first bug), insert-then-append for a pause — but WITHOUT a cross-store tx, so a
// mid-sequence failure is not rolled back. runner.New defaults to it when
// Deps.ResumeCommitter is nil.
type splitResumeCommitter struct {
	conv  ConversationStore
	pause PauseStore
}

func newSplitResumeCommitter(conv ConversationStore, pause PauseStore) *splitResumeCommitter {
	return &splitResumeCommitter{conv: conv, pause: pause}
}

func (s *splitResumeCommitter) CommitResume(ctx context.Context, claim ResumeClaim) error {
	if err := s.pause.MarkResumed(ctx, claim.Token, claim.Answer); err != nil {
		return err
	}
	return s.conv.AppendTurn(ctx, claim.Turn)
}

func (s *splitResumeCommitter) CommitResumeBatch(ctx context.Context, claims []ResumeClaim) error {
	if len(claims) == 0 {
		return nil
	}
	answers := make(map[string]askuser.ResumeAnswer, len(claims))
	for _, c := range claims {
		answers[c.Token] = c.Answer
	}
	if err := s.pause.MarkResumedBatch(ctx, answers); err != nil {
		return err
	}
	for _, c := range claims {
		if err := s.conv.AppendTurn(ctx, c.Turn); err != nil {
			return err
		}
	}
	return nil
}

func (s *splitResumeCommitter) CommitPause(ctx context.Context, assistantTurn conversations.AppendTurnParams, pauses []askuser.InsertParams) error {
	if len(pauses) == 0 {
		return nil
	}
	for _, ins := range pauses {
		if err := s.pause.Insert(ctx, ins); err != nil {
			return err
		}
	}
	return s.conv.AppendTurn(ctx, assistantTurn)
}
