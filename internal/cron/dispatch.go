package cron

// dispatch.go is the TaskKind → Handler routing seam the 10-03 tick loop drives (the
// Dispatcher interface). There is NO big dispatch switch (Slice 0.9): each TaskKind
// is one Handler exposing a HandlerMeta, looked up in a kind→handler map. The
// dispatcher owns the run lifecycle the handlers stay free of: it runs the handler,
// writes the audit summary via CompleteRun (idempotent on completed_with_hash, SC#2),
// notifies the per-task route (D-19) — on success AND on failure (D-21) — and rides
// RISKY/DESTRUCTIVE through the Notifier as an immediate alert (D-27).
//
// Handler is a cron-LOCAL interface (consumer-declared, the 10-04 taskStore pattern):
// the real per-kind impls live in internal/cron/handlers (which imports
// internal/agent/tools — and tools imports cron, so cron CANNOT import handlers
// without a cycle). The composition root (cmd/aura, which imports both) adapts each
// handlers.Handler into this interface and supplies the map. dispatch.go therefore
// unit-tests against fake handlers with zero LLM/tool wiring.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/scoring"
)

// HandlerMeta is the per-kind static contract (Slice 0.9): the kind it serves, its
// wall-clock budget, and whether a missed window reschedules it on recovery (D-18).
type HandlerMeta struct {
	Kind                  TaskKind
	MaxDuration           time.Duration
	ReschedulesOnRecovery bool
}

// Job is the plain-value input a Handler runs against (the cron-local mirror of
// handlers.Job; the composition-root adapter translates between them).
type Job struct {
	Payload    []byte
	StepBudget int
	RunID      string
	// MissedSince is non-zero only for a boot catch-up run (D-18): the original
	// slipped fire. A handler (backup) uses it to decide whether to emit the SC#3
	// missed-past-the-window alert; it is the zero value for a normal tick run.
	MissedSince time.Time
}

// Handler is the per-kind unit of work the dispatcher routes to. Run returns the
// audit summary + a terminal error (nil on success).
type Handler interface {
	Meta() HandlerMeta
	Run(ctx context.Context, job Job) (summary string, err error)
}

// RunCompleter writes a terminal run state — the run-lifecycle seam the dispatcher
// needs (the concrete *Store satisfies it; unit tests inject a fake without a pool).
type RunCompleter interface {
	CompleteRun(ctx context.Context, p CompleteRunParams) error
}

var _ RunCompleter = (*Store)(nil)

// PendingNotificationStore is the durable notification queue seam. *Store
// satisfies it; tests can inject a small fake without a database.
type PendingNotificationStore interface {
	InsertPendingNotification(ctx context.Context, p InsertPendingNotificationParams) (PendingNotification, error)
	SweepDueNotifications(ctx context.Context, attemptBound, limit int) ([]PendingNotification, error)
	MarkNotificationDelivered(ctx context.Context, id string) error
	MarkNotificationFailed(ctx context.Context, id, lastErr string) error
}

var _ PendingNotificationStore = (*Store)(nil)

// DispatchDeps carries the run lifecycle collaborators: the store (run completion),
// the Notifier (delivery), the alert threshold, and the quiet-hours predicate.
type DispatchDeps struct {
	Store             RunCompleter
	NotificationStore PendingNotificationStore
	Notifier          Notifier
	AlertThreshold    scoring.RiskTier
	// QuietHours reports whether the current wall time is inside the deferral window
	// (the scheduler's DuringQuietHours predicate, D-23). Nil = never quiet.
	QuietHours func(tz string) bool
	// QuietHoursEnd returns the wall-clock instant when the current quiet-hours
	// window ends. It is paired with QuietHours so deferred notifications get a
	// durable notify_after instead of being dropped.
	QuietHoursEnd func(tz string) (time.Time, bool)
	// ChannelDeliverer prefers the origin channel over the per-task route (Phase 20
	// R4/R7). Nil → legacy route-only behavior (the regression guard); the
	// composition root adapts *channels.Registry onto it.
	ChannelDeliverer ChannelDeliverer
	// PreferOriginChannel is the AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL kill-switch,
	// resolved once at the composition root (default true). False → byte-identical
	// legacy route-only behavior even when a ChannelDeliverer is wired (D-03).
	PreferOriginChannel bool
}

// Dispatch routes a claimed task to its handler and owns the run lifecycle. It
// satisfies the Dispatcher seam the tick loop calls on the held conn (claim.go).
type Dispatch struct {
	deps     DispatchDeps
	handlers map[TaskKind]Handler
}

var _ Dispatcher = (*Dispatch)(nil)

// ReschedulesOnRecovery reports a kind's HandlerMeta.ReschedulesOnRecovery (M-g) — the
// handler-meta lookup seam the boot catch-up consults (recover.go). An unknown kind
// (no handler) reports false: the dispatcher would fail that kind loud at dispatch
// time anyway, so the catch-up should not re-fire a kind it cannot route. The
// composition root passes this method as SchedulerConfig.ReschedulesOnRecovery.
func (d *Dispatch) ReschedulesOnRecovery(kind TaskKind) bool {
	h, ok := d.handlers[kind]
	if !ok {
		return false
	}
	return h.Meta().ReschedulesOnRecovery
}

// NewDispatch builds the dispatcher from the kind→handler map (no dispatch switch)
// and the run-lifecycle deps. The map is supplied by the composition root, which
// adapts the internal/cron/handlers impls into the cron-local Handler interface.
func NewDispatch(handlers map[TaskKind]Handler, deps DispatchDeps) *Dispatch {
	if deps.AlertThreshold == "" {
		deps.AlertThreshold = scoring.Risky
	}
	if deps.NotificationStore == nil {
		if store, ok := deps.Store.(PendingNotificationStore); ok {
			deps.NotificationStore = store
		}
	}
	return &Dispatch{deps: deps, handlers: handlers}
}

// Dispatch runs the handler for task.Kind, writes the terminal run state, and
// notifies. A missing handler for an unknown kind is a terminal failed run (never a
// silent drop, D-21). The held conn (claim.go) keeps the advisory lock for the run's
// lifetime; CompleteRun writes through the pool but the lock is unaffected.
func (d *Dispatch) Dispatch(ctx context.Context, task Task, c *Claim) error {
	h, ok := d.handlers[task.Kind]
	if !ok {
		err := fmt.Errorf("no handler for kind %q", task.Kind)
		d.complete(ctx, task, c.RunID, "failed", "", err)
		d.notify(ctx, task, c.RunID, "", err)
		return err
	}

	job := Job{Payload: task.Payload, StepBudget: task.StepBudget, RunID: c.RunID, MissedSince: c.MissedSince}
	summary, runErr := h.Run(ctx, job)

	status := "completed"
	if runErr != nil {
		status = "failed"
	}
	d.complete(ctx, task, c.RunID, status, summary, runErr)
	d.notify(ctx, task, c.RunID, summary, runErr)
	return runErr
}

// completeRunTimeout bounds the terminal run-state write on the detached ctx (M-h): a
// short deadline so a wedged DB during shutdown can never hang the drain, while still
// leaving ample room for the single UPDATE to land.
const completeRunTimeout = 5 * time.Second

// complete writes the terminal run state. The idempotency key is the run id (each
// claim opens a unique run; a redelivered completion of the SAME run trips the
// completed_with_hash UNIQUE constraint and is swallowed as ErrAlreadyRunning, SC#2).
//
// The write runs on a ctx DETACHED from the (possibly signal-cancelled) root via
// context.WithoutCancel + a short deadline (M-h). On graceful shutdown the dispatch
// ctx is already cancelled, and pgx rejects a query on a cancelled ctx — so the
// terminal write would only be logged and the run would stay 'running' until the 90s
// orphan scan reclaimed it. WithoutCancel preserves any tracing values while severing
// the cancellation, so the run-state ledger stays accurate even mid-shutdown.
func (d *Dispatch) complete(ctx context.Context, task Task, runID, status, summary string, runErr error) {
	lastErr := ""
	if runErr != nil {
		lastErr = runErr.Error()
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), completeRunTimeout)
	defer cancel()
	err := d.deps.Store.CompleteRun(writeCtx, CompleteRunParams{
		RunID:         runID,
		Status:        status,
		Summary:       summary,
		LastError:     lastErr,
		CompletedHash: runID,
	})
	if err != nil {
		slog.Warn("dispatch complete run", "task", task.ID, "run", runID, "status", status, "err", err)
	}
}

// notify delivers the run outcome to the per-task route (D-19). It notifies on
// success (the summary) AND on failure (D-21, "<kind> failed: <LastError>"). A
// RISKY/DESTRUCTIVE task rides the same route as an immediate alert (D-27). A
// non-destructive, non-immediate notification inside quiet hours is deferred —
// skipped this tick (D-23); a reminder still fires (its delivery IS the task, not an
// advisory notification).
func (d *Dispatch) notify(ctx context.Context, task Task, runID, summary string, runErr error) {
	if d.deps.Notifier == nil {
		return
	}
	tier := d.taskTier(task)
	text := summary
	if runErr != nil {
		text = fmt.Sprintf("%s failed: %s", task.Kind, runErr.Error())
	}

	immediate := scoring.RequiresImmediateAlert(tier, d.deps.AlertThreshold)
	if !immediate && runErr == nil && task.Kind != KindReminder && d.deferred(task) {
		notifyAfter := time.Now().UTC()
		if end, ok := d.deferredUntil(task); ok {
			notifyAfter = end
		}
		if err := d.insertPendingNotification(ctx, task, runID, text, notifyAfter, "pending", 0, ""); err != nil {
			slog.Warn("persist deferred scheduler notification", "task", task.ID, "run", runID, "err", err)
		}
		slog.Info("notification deferred to quiet-hours window end", "task", task.ID)
		return
	}
	// Prefer the origin channel (R4/R7): a reminder set in a Telegram DM lands back
	// in that DM. deliverToOrigin returns true when delivery is the channel's concern
	// (delivered, or owns-but-failed-and-queued) — only fall through to the per-task
	// route when no channel owns the identity / an explicit route was set / the
	// kill-switch is off.
	if d.deliverToOrigin(ctx, task, runID, text) {
		return
	}
	if err := d.deps.Notifier.Notify(ctx, NotifyRoute(task.NotifyRoute), "", text); err != nil {
		slog.Warn("dispatch notify undelivered (bound-retry on a later tick)", "task", task.ID, "err", err)
		if perr := d.insertPendingNotification(ctx, task, runID, text, time.Now().UTC(), "failed", 0, err.Error()); perr != nil {
			slog.Warn("persist failed scheduler notification", "task", task.ID, "run", runID, "err", perr)
		}
	}
}

// deferred reports whether a non-destructive notification should defer for this task
// under quiet hours (D-23). A nil QuietHours predicate never defers.
func (d *Dispatch) deferred(task Task) bool {
	if d.deps.QuietHours == nil {
		return false
	}
	return d.deps.QuietHours(task.TZ)
}

func (d *Dispatch) deferredUntil(task Task) (time.Time, bool) {
	if d.deps.QuietHoursEnd == nil {
		return time.Time{}, false
	}
	return d.deps.QuietHoursEnd(task.TZ)
}

func (d *Dispatch) insertPendingNotification(
	ctx context.Context,
	task Task,
	runID, body string,
	notifyAfter time.Time,
	status string,
	attempts int,
	lastErr string,
) error {
	if d.deps.NotificationStore == nil {
		return nil
	}
	_, err := d.deps.NotificationStore.InsertPendingNotification(ctx, InsertPendingNotificationParams{
		RunID:       runID,
		NotifyRoute: task.NotifyRoute,
		Body:        body,
		NotifyAfter: notifyAfter,
		Attempts:    attempts,
		LastError:   lastErr,
		Status:      status,
	})
	return err
}

// taskTier recomputes the risk tier at dispatch from the task's kind + payload (the
// schedule-time gate already routed Destructive to pending_approval; this recompute
// drives the immediate-alert decision, D-27). scoring takes the threshold as an
// argument, never reads env (the purity contract).
func (d *Dispatch) taskTier(task Task) scoring.RiskTier {
	return scoring.ComputeTaskTier(scoring.TaskArgs{
		Kind:         string(task.Kind),
		ScheduleKind: string(task.ScheduleKind),
		Payload:      task.Payload,
	})
}

const (
	pendingNotificationAttemptBound = 3
	pendingNotificationSweepLimit   = 50
)

func (d *Dispatch) sweepNotifications(ctx context.Context) error {
	if d.deps.NotificationStore == nil || d.deps.Notifier == nil {
		return nil
	}
	rows, err := d.deps.NotificationStore.SweepDueNotifications(ctx, pendingNotificationAttemptBound, pendingNotificationSweepLimit)
	if err != nil {
		return err
	}
	for _, n := range rows {
		if err := d.deps.Notifier.Notify(ctx, NotifyRoute(n.NotifyRoute), "", n.Body); err != nil {
			if markErr := d.deps.NotificationStore.MarkNotificationFailed(ctx, n.ID, err.Error()); markErr != nil {
				slog.Warn("mark scheduler notification failed", "notification", n.ID, "err", markErr)
			}
			continue
		}
		if err := d.deps.NotificationStore.MarkNotificationDelivered(ctx, n.ID); err != nil {
			slog.Warn("mark scheduler notification delivered", "notification", n.ID, "err", err)
		}
	}
	return nil
}
