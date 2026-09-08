package steer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrWorkerScope rejects coordinates that cannot name an owned worker incarnation.
var ErrWorkerScope = errors.New("steer: invalid worker scope")

type workerScope struct {
	owner  pgtype.UUID
	worker pgtype.Text
	run    pgtype.Text
}

func newWorkerScope(ctx context.Context, conv, childID, runID string) (workerScope, error) {
	if ctx == nil || childID == "" || len(childID) > 128 {
		return workerScope{}, ErrWorkerScope
	}
	owner, err := uuid.Parse(identityctx.IdentityID(ctx))
	if err != nil {
		return workerScope{}, ErrWorkerScope
	}
	if _, err := uuid.Parse(conv); err != nil {
		return workerScope{}, ErrWorkerScope
	}
	run, ok := strings.CutPrefix(runID, "run-")
	if !ok {
		return workerScope{}, ErrWorkerScope
	}
	if _, err := uuid.Parse(run); err != nil {
		return workerScope{}, ErrWorkerScope
	}
	return workerScope{
		owner:  pgtype.UUID{Bytes: owner, Valid: true},
		worker: pgtype.Text{String: childID, Valid: true},
		run:    pgtype.Text{String: runID, Valid: true},
	}, nil
}

// PushWorker binds the queued correction to the host-resolved execution. The
// operation key also covers a crash after insertion but before the HTTP receipt.
func (s *PostgresStore) PushWorker(ctx context.Context, conv, childID, runID, text string) error {
	scope, err := newWorkerScope(ctx, conv, childID, runID)
	if err != nil {
		return err
	}
	op, ok := idempotency.OperationFromContext(ctx)
	if !ok || op.Key.IdentityID != uuidString(scope.owner) {
		return idempotency.ErrOperationContext
	}
	key := "worker-steer:" + string(op.Key.Scope) + ":" + op.Key.Key
	return s.pushScoped(ctx, conv, "cockpit", text, KindSteer, "", key, scope)
}

// WorkerInbox implements the existing agent drain contract without treating the
// flat worker session as a conversation UUID or exposing its rows to the parent.
type WorkerInbox struct {
	store          *PostgresStore
	ctx            context.Context
	conversationID string
	scope          workerScope
}

// ForWorker binds the existing drain interface to one host-resolved execution.
func (s *PostgresStore) ForWorker(ctx context.Context, conv, childID, runID string) (*WorkerInbox, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("steer: worker store is not configured")
	}
	scope, err := newWorkerScope(ctx, conv, childID, runID)
	if err != nil {
		return nil, err
	}
	return &WorkerInbox{store: s, ctx: ctx, conversationID: conv, scope: scope}, nil
}

// Drain consumes only this incarnation's corrections, never a parent or sibling inbox.
func (i *WorkerInbox) Drain(sessionID string) []Message {
	if i == nil || sessionID != i.conversationID+"-swarm-"+i.scope.worker.String {
		return nil
	}
	return i.store.drain(i.ctx, i.conversationID, i.scope)
}

// Close rejects unapplied corrections after the registry has closed control admission.
func (i *WorkerInbox) Close() error {
	if i == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(i.ctx), 5*time.Second)
	defer cancel()
	return db.WithTx(ctx, i.store.pool, func(q *sqlc.Queries) error {
		if err := q.LockSteerConversation(ctx, i.conversationID); err != nil {
			return err
		}
		_, err := q.ExpireWorkerRunSteers(ctx, sqlc.ExpireWorkerRunSteersParams{
			IdentityID: i.scope.owner, ConversationID: i.conversationID, TargetRunID: i.scope.run,
		})
		return err
	})
}

// WorkerReceipt separates queue acceptance from actual delivery into worker context.
type WorkerReceipt struct {
	ID        string     `json:"id"`
	RunID     string     `json:"run_id"`
	Text      string     `json:"text"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

// WorkerHistory returns at most the latest hundred receipts in chronological order.
func (s *PostgresStore) WorkerHistory(ctx context.Context, conv, childID string) ([]WorkerReceipt, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("steer: worker store is not configured")
	}
	if ctx == nil || childID == "" {
		return nil, ErrWorkerScope
	}
	owner, err := uuid.Parse(identityctx.IdentityID(ctx))
	if err != nil {
		return nil, ErrWorkerScope
	}
	rows, err := sqlc.New(s.pool).ListWorkerSteers(ctx, sqlc.ListWorkerSteersParams{
		IdentityID: pgtype.UUID{Bytes: owner, Valid: true}, ConversationID: conv,
		TargetWorkerID: pgtype.Text{String: childID, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	result := make([]WorkerReceipt, 0, len(rows))
	for _, row := range slices.Backward(rows) {
		receipt := WorkerReceipt{ID: uuidString(row.ID), RunID: row.TargetRunID.String, Text: row.Body, Status: "accepted", CreatedAt: row.CreatedAt.Time}
		switch {
		case row.DrainedAt.Valid:
			receipt.Status = "applied"
			receipt.AppliedAt = &row.DrainedAt.Time
		case row.ExpiredAt.Valid:
			receipt.Status = "rejected"
			receipt.Reason = row.ExpiryReason.String
		case row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(s.now()):
			receipt.Status = "rejected"
			receipt.Reason = "expired"
		}
		result = append(result, receipt)
	}
	return result, nil
}
