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

// DispatchDeps carries the run lifecycle collaborators: the store (run completion),
// the Notifier (delivery), the alert threshold, and the quiet-hours predicate.
type DispatchDeps struct {
	Store          RunCompleter
	Notifier       Notifier
	AlertThreshold scoring.RiskTier
	// QuietHours reports whether the current wall time is inside the deferral window
	// (the scheduler's DuringQuietHours predicate, D-23). Nil = never quiet.
	QuietHours func(tz string) bool
}

// Dispatch routes a claimed task to its handler and owns the run lifecycle. It
// satisfies the Dispatcher seam the tick loop calls on the held conn (claim.go).
type Dispatch struct {
	deps     DispatchDeps
	handlers map[TaskKind]Handler
}

var _ Dispatcher = (*Dispatch)(nil)

// NewDispatch builds the dispatcher from the kind→handler map (no dispatch switch)
// and the run-lifecycle deps. The map is supplied by the composition root, which
// adapts the internal/cron/handlers impls into the cron-local Handler interface.
func NewDispatch(handlers map[TaskKind]Handler, deps DispatchDeps) *Dispatch {
	if deps.AlertThreshold == "" {
		deps.AlertThreshold = scoring.Risky
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
		d.notify(ctx, task, "", err)
		return err
	}

	job := Job{Payload: task.Payload, StepBudget: task.StepBudget, RunID: c.RunID}
	summary, runErr := h.Run(ctx, job)

	status := "completed"
	if runErr != nil {
		status = "failed"
	}
	d.complete(ctx, task, c.RunID, status, summary, runErr)
	d.notify(ctx, task, summary, runErr)
	return runErr
}

// complete writes the terminal run state. The idempotency key is the run id (each
// claim opens a unique run; a redelivered completion of the SAME run trips the
// completed_with_hash UNIQUE constraint and is swallowed as ErrAlreadyRunning, SC#2).
func (d *Dispatch) complete(ctx context.Context, task Task, runID, status, summary string, runErr error) {
	lastErr := ""
	if runErr != nil {
		lastErr = runErr.Error()
	}
	err := d.deps.Store.CompleteRun(ctx, CompleteRunParams{
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
func (d *Dispatch) notify(ctx context.Context, task Task, summary string, runErr error) {
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
		slog.Info("notification deferred to quiet-hours window end", "task", task.ID)
		return
	}
	if err := d.deps.Notifier.Notify(ctx, NotifyRoute(task.NotifyRoute), "", text); err != nil {
		slog.Warn("dispatch notify undelivered (bound-retry on a later tick)", "task", task.ID, "err", err)
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
