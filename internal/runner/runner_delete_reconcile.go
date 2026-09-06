package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

// ConversationProjectionIdentities lists the PostgreSQL identities whose
// authoritative conversations must be replayed into their tenant graphs.
type ConversationProjectionIdentities interface {
	IdentityIDs(context.Context) ([]string, error)
}

// ReasoningRetentionStore is the bounded tenant-aware expiry seam.
type ReasoningRetentionStore interface {
	DeleteExpiredReasoning(context.Context, string, time.Time, int) (int, error)
}

// DeleteReconciler resumes export-delete operations from their stored operation
// reservation after cancellation, transient failure, or process restart.
type DeleteReconciler struct {
	runner               *Runner
	interval             time.Duration
	projector            *ConversationProjector
	projectionIdentities ConversationProjectionIdentities
	reasoningRetention   ReasoningRetentionStore
	reasoningIdentities  ConversationProjectionIdentities
	reasoningBatch       int

	wg        sync.WaitGroup
	stop      chan struct{}
	once      sync.Once
	startOnce sync.Once
	cancelMu  sync.Mutex
	cancel    context.CancelFunc
}

// SetReasoningRetention wires the single boot-owned reasoning expiry lifecycle.
func (r *DeleteReconciler) SetReasoningRetention(
	store ReasoningRetentionStore,
	identities ConversationProjectionIdentities,
	batchSize int,
) {
	if r != nil {
		r.reasoningRetention = store
		r.reasoningIdentities = identities
		r.reasoningBatch = batchSize
	}
}

// ReconcileReasoningRetention expires one bounded page for every authoritative identity.
func (r *DeleteReconciler) ReconcileReasoningRetention(ctx context.Context, now time.Time) error {
	if r == nil || r.reasoningRetention == nil || r.reasoningIdentities == nil || r.reasoningBatch <= 0 {
		return nil
	}
	identityIDs, err := r.reasoningIdentities.IdentityIDs(ctx)
	if err != nil {
		return fmt.Errorf("reasoning retention: list authoritative identities: %w", err)
	}
	clean, invalid := normalizeProjectionIdentities(identityIDs)
	failures := append([]error(nil), invalid...)
	for _, identityID := range clean {
		if _, err := r.reasoningRetention.DeleteExpiredReasoning(
			ctx, identityID, now.UTC(), r.reasoningBatch,
		); err != nil {
			failures = append(failures, fmt.Errorf("reasoning retention identity %s: %w", identityID, err))
		}
	}
	return errors.Join(failures...)
}

// NewDeleteReconciler constructs the boot-one-shot and interval recovery worker.
func NewDeleteReconciler(runner *Runner, interval time.Duration) *DeleteReconciler {
	return &DeleteReconciler{runner: runner, interval: interval, stop: make(chan struct{})}
}

// SetConversationProjector joins derived deletion to reserved-delete recovery.
// Projection failure is reported but never rolls back PostgreSQL authority;
// full replay pruning remains the crash/outage repair path.
func (r *DeleteReconciler) SetConversationProjector(projector *ConversationProjector) {
	if r != nil {
		r.projector = projector
	}
}

// SetConversationProjection wires the authoritative identity roster used by
// boot and periodic full replay.
func (r *DeleteReconciler) SetConversationProjection(
	projector *ConversationProjector,
	identities ConversationProjectionIdentities,
) {
	if r != nil {
		r.projector = projector
		r.projectionIdentities = identities
	}
}

// ReconcileConversationProjection repairs derived graph lag from PostgreSQL.
func (r *DeleteReconciler) ReconcileConversationProjection(ctx context.Context) error {
	if r == nil || r.projector == nil || r.projectionIdentities == nil {
		return nil
	}
	identityIDs, err := r.projectionIdentities.IdentityIDs(ctx)
	if err != nil {
		return fmt.Errorf("conversation projection: list authoritative identities: %w", err)
	}
	clean, failures := normalizeProjectionIdentities(identityIDs)
	for _, identityID := range clean {
		if err := r.projector.Reconcile(ctx, identityID); err != nil {
			failures = append(failures, fmt.Errorf("conversation projection identity %s: %w", identityID, err))
		}
	}
	return errors.Join(failures...)
}

func normalizeProjectionIdentities(identityIDs []string) ([]string, []error) {
	clean := make([]string, 0, len(identityIDs))
	seen := make(map[string]struct{}, len(identityIDs))
	var failures []error
	for _, identityID := range identityIDs {
		identityID = strings.TrimSpace(identityID)
		if identityID == "" {
			failures = append(failures, errors.New("authoritative identity is empty"))
			continue
		}
		if _, duplicate := seen[identityID]; duplicate {
			continue
		}
		seen[identityID] = struct{}{}
		clean = append(clean, identityID)
	}
	sort.Strings(clean)
	return clean, failures
}

// Start launches one immediate recovery pass followed by periodic reconciliation.
func (r *DeleteReconciler) Start(ctx context.Context) {
	if r == nil || r.interval <= 0 {
		return
	}
	hasDeleteRecovery := false
	if r.runner != nil {
		_, hasDeleteRecovery = r.runner.Conv.(reservedDeleteRecoveryStore)
	}
	hasProjectionRecovery := r.projector != nil && r.projectionIdentities != nil
	hasReasoningRetention := r.reasoningRetention != nil && r.reasoningIdentities != nil && r.reasoningBatch > 0
	if !hasDeleteRecovery && !hasProjectionRecovery && !hasReasoningRetention {
		return
	}
	r.startOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		r.cancelMu.Lock()
		r.cancel = cancel
		r.cancelMu.Unlock()
		r.wg.Go(func() {
			select {
			case <-r.stop:
				return
			default:
			}
			r.reconcile(workerCtx)
			ticker := time.NewTicker(r.interval)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-r.stop:
					return
				case <-ticker.C:
					r.reconcile(workerCtx)
				}
			}
		})
	})
}

// Stop idempotently joins the recovery worker under a bounded wait.
func (r *DeleteReconciler) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		close(r.stop)
		r.cancelMu.Lock()
		cancel := r.cancel
		r.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
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
	ctx, cancel := context.WithTimeout(parent, conversationDeleteFinalizeTimeout)
	defer cancel()
	if r.runner != nil {
		if _, ok := r.runner.Conv.(reservedDeleteRecoveryStore); ok {
			if _, err := r.runner.reconcileReservedConversationDeletesWithProjection(
				ctx, deleteRecoveryBatch, r.projector,
			); err != nil {
				slog.Warn("delete recovery: reconcile reserved conversations", "err", err)
			}
		}
	}
	if err := r.ReconcileConversationProjection(ctx); err != nil {
		slog.Warn("conversation projection reconciliation failed", "err", collapseJoined(err))
	}
	if err := r.ReconcileReasoningRetention(ctx, time.Now().UTC()); err != nil {
		slog.Warn("reasoning retention reconciliation failed", "err", collapseJoined(err))
	}
}

func (r *Runner) reconcileReservedConversationDeletes(ctx context.Context, limit int32) (int, error) {
	return r.reconcileReservedConversationDeletesWithProjection(ctx, limit, nil)
}

func (r *Runner) reconcileReservedConversationDeletesWithProjection(
	ctx context.Context,
	limit int32,
	projector *ConversationProjector,
) (int, error) {
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
		if projector != nil {
			if projectionErr := projector.DeleteConversation(ctx, item.IdentityID, item.ConversationID); projectionErr != nil {
				failures = append(failures, fmt.Errorf("conversation %s projection: %w", item.ConversationID, projectionErr))
			}
		}
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
