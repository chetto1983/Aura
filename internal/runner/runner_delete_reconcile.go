package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
)

const (
	deleteRecoveryBatch       = 64
	deleteRecoveryStopTimeout = 30 * time.Second
)

type reservedDeleteRecoveryStore interface {
	ListReservedDeletes(context.Context, int32) ([]conversations.ReservedDelete, error)
}

// DeleteReconciler resumes export-delete operations from their stored operation
// reservation after cancellation, transient failure, or process restart.
type DeleteReconciler struct {
	runner   *Runner
	interval time.Duration

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

// NewDeleteReconciler constructs the boot-one-shot and interval recovery worker.
func NewDeleteReconciler(runner *Runner, interval time.Duration) *DeleteReconciler {
	return &DeleteReconciler{runner: runner, interval: interval, stop: make(chan struct{})}
}

// Start launches recovery when the store exposes durable reservations.
func (r *DeleteReconciler) Start(ctx context.Context) {
	if r == nil || r.runner == nil || r.interval <= 0 {
		return
	}
	if _, ok := r.runner.Conv.(reservedDeleteRecoveryStore); !ok {
		return
	}
	r.wg.Go(func() {
		r.reconcile(ctx)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stop:
				return
			case <-ticker.C:
				r.reconcile(ctx)
			}
		}
	})
}

// Stop idempotently joins the recovery worker under a bounded wait.
func (r *DeleteReconciler) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() { close(r.stop) })
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(deleteRecoveryStopTimeout):
	}
}

func (r *DeleteReconciler) reconcile(parent context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), conversationDeleteFinalizeTimeout)
	defer cancel()
	if _, err := r.runner.reconcileReservedConversationDeletes(ctx, deleteRecoveryBatch); err != nil {
		slog.Warn("delete recovery: reconcile reserved conversations", "err", err)
	}
}

func (r *Runner) reconcileReservedConversationDeletes(ctx context.Context, limit int32) (int, error) {
	store, ok := r.Conv.(reservedDeleteRecoveryStore)
	if !ok {
		return 0, errors.New("delete recovery: reserved conversation store unavailable")
	}
	items, err := store.ListReservedDeletes(ctx, limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	var failures []error
	for _, item := range items {
		affected, resumeErr := r.resumeReservedConversationDelete(ctx, item.IdentityID, item.ConversationID, item.Reservation)
		if resumeErr != nil {
			failures = append(failures, fmt.Errorf("conversation %s: %w", item.ConversationID, resumeErr))
			continue
		}
		if affected == 1 {
			completed++
		}
	}
	return completed, errors.Join(failures...)
}
