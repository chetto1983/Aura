// reconcile.go is the crash-orphan reconciler (D-01d / GATE-03 durability +
// GATE-04 recovery): the background worker that closes a `start`-without-`end`
// reservation left by a crash by APPENDING a terminal `end` fact — never by
// re-invoking the orphaned tool (its mutating side effect may already have fired).
// Its lifecycle mirrors conversations.Sweeper (boot one-shot + interval tick,
// bounded Stop join, goleak-clean); its age-grace idiom mirrors orphan_scan.go's
// sidecarOrphanGrace. This file writes ledger rows only — no migration, no table.
package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/toolinvocations"
)

// reconcileStopJoinTimeout bounds Stop()'s wg.Wait() drain so a reconcile tick that
// wedges on a slow query cannot wedge the daemon shutdown (mirrors the sweeper's
// stopJoinTimeout). A tick already in flight when Stop fires runs to completion;
// this only bounds how long Stop waits to join it.
const reconcileStopJoinTimeout = 30 * time.Second

// reservationOrphanGrace is the crash-signal window: a `start`-without-`end`
// reservation younger than this is presumed still in-flight (a legitimately slow
// call), so the reconciler leaves it alone. It is a named const SMALLER than
// orphan_scan.go's tmpTTL (24h) — a stuck in-flight mutating call, not a scratch
// artifact TTL (Open Q4). It is the FLOOR of the effective grace, not the whole
// story: effectiveGrace expands it above the resolved run lifetime (see below).
const reservationOrphanGrace = 30 * time.Minute

// reservationOrphanMargin is the safety cushion added ABOVE the resolved single-run
// tool-execution lifetime when that lifetime would otherwise approach or exceed
// reservationOrphanGrace. It guarantees effectiveGrace strictly exceeds the run
// lifetime even after an operator raises a loop knob (the collision-impossibility
// invariant below).
const reservationOrphanMargin = 5 * time.Minute

// defaultReconcileInterval is the tick cadence the serve composition root wires when
// it does not override it: often enough that a crash-orphan is closed within a bounded
// window, sparse enough that the anti-join is negligible load. A non-positive interval
// DISABLES the worker (Start launches no goroutine), mirroring the sweeper.
const defaultReconcileInterval = 10 * time.Minute

// inFlightReconcileStore is the narrow read+append seam the Reconciler needs.
// *toolinvocations.Store satisfies it. The reconciler NEVER updates or deletes — it
// only lists start∧¬end candidates, re-checks for a real end, and appends a synthetic
// end (the append-only triggers reject anything else).
type inFlightReconcileStore interface {
	ListInFlightBefore(ctx context.Context, cutoff time.Time) ([]toolinvocations.Event, error)
	GetEnd(ctx context.Context, conversationID, requestID, toolCallID string) (*toolinvocations.Event, error)
	Insert(ctx context.Context, e toolinvocations.Event) error
}

// effectiveGrace returns the age past which a start∧¬end reservation is a PROVABLE
// crash-orphan: max(reservationOrphanGrace, maxToolExecWindow + reservationOrphanMargin).
//
// DATA-INTEGRITY INVARIANT (checker WARNING 4): the effective grace MUST exceed the
// maximum possible single-run tool-execution lifetime. A start∧¬end older than it can
// therefore never be a still-running (or legitimately slow) tool — its real `end` can
// never race the reconciler's synthetic end into the ON CONFLICT DO NOTHING first-writer
// slot (35-04's semantics, unchanged). So a legitimately slow-but-within-lifetime mutating
// tool's real outcome is NEVER overwritten by an indeterminate error: its reservation is
// younger than the effective grace and the reconciler skips it.
//
// The source bounds are the two AURA_LOOP_* knobs the serve root resolves and passes in
// as maxToolExecWindow = max(nodeTimeout, wallclock):
//   - AURA_LOOP_NODE_TIMEOUT_SEC — the per-tool soft ctx timeout (budget.NodeTimeout());
//     default 0 = DISABLED (there is NO hard per-tool ctx deadline by default).
//   - AURA_LOOP_MAX_WALLCLOCK_SEC — the whole-run cap; default 300s
//     (agent.defaultBudgetWallclockSec).
//
// effectiveGrace(w) > w for every w (margin > 0 when the lifetime dominates; the 30-min
// floor dominates otherwise), so the collision is impossible for any within-lifetime
// reservation, and the grace auto-expands if an operator raises either knob.
//
// HONEST RESIDUAL: because AURA_LOOP_NODE_TIMEOUT_SEC defaults to 0, the wallclock bounds
// run PROGRESS via ConsumeStep refusing new steps, not by cancelling an in-flight tool. A
// mutating tool genuinely blocked past the effective grace is therefore marked
// indeterminate — the correct conservative D-01d outcome: it is never re-invoked, the
// reservation still blocks a double-apply, and a later replay returns a safe indeterminate
// rather than re-executing the side effect.
func effectiveGrace(maxToolExecWindow time.Duration) time.Duration {
	if lifetime := maxToolExecWindow + reservationOrphanMargin; lifetime > reservationOrphanGrace {
		return lifetime
	}
	return reservationOrphanGrace
}

// Reconciler is the interval-driven background worker that closes crash-orphaned
// reservations by appending a terminal indeterminate `end` fact. Its lifecycle mirrors
// conversations.Sweeper VERBATIM: Start launches one goroutine (a boot one-shot then an
// interval tick), Stop joins it under a bounded wait (goleak-clean), and a cancelled
// Start ctx winds it down too. A nil *Reconciler or a non-positive interval is a no-op.
type Reconciler struct {
	store             inFlightReconcileStore
	interval          time.Duration
	maxToolExecWindow time.Duration

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

// NewReconciler builds a reconciler over the append-only ledger store. It does NOT start
// the worker — the caller wires Start into the daemon boot and Stop into its shutdown.
// interval <= 0 yields a reconciler whose Start no-ops (disabled); maxToolExecWindow is
// the resolved max(nodeTimeout, wallclock) that feeds effectiveGrace.
func NewReconciler(store inFlightReconcileStore, interval, maxToolExecWindow time.Duration) *Reconciler {
	return &Reconciler{
		store:             store,
		interval:          interval,
		maxToolExecWindow: maxToolExecWindow,
		stop:              make(chan struct{}),
	}
}

// Start launches the boot one-shot reconcile followed by the interval tick loop in one
// goroutine. A non-positive interval or a nil store launches nothing (the worker is
// disabled). The goroutine exits on Stop() or ctx cancellation, whichever comes first —
// Stop afterward is still a clean join.
func (r *Reconciler) Start(ctx context.Context) {
	if r == nil || r.interval <= 0 || r.store == nil {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		// Boot one-shot: a daemon that just came up after a crash reconciles the
		// orphans the crash left before the first tick period elapses (mirrors the
		// sweeper's boot ScanOrphans one-shot).
		r.reconcileOrphans(ctx)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stop:
				return
			case <-ticker.C:
				r.reconcileOrphans(ctx)
			}
		}
	}()
}

// Stop signals the worker to exit and joins it under a bounded wait so a hung tick
// cannot wedge shutdown. It is idempotent (safe when Start was never invoked, e.g. a
// disabled interval) and safe to call multiple times.
func (r *Reconciler) Stop() {
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
	case <-time.After(reconcileStopJoinTimeout):
	}
}

// reconcileOrphans lists every start∧¬end reservation older than the effective grace and,
// for each, appends a terminal indeterminate `end` fact — APPEND-only, never UPDATE
// (D-01d; the append-only triggers reject a mutation). The candidate set is
// provenance-agnostic: an operator-approved-and-resumed mutating call produces the SAME
// single start∧¬end shape as an auto-allowed one (35-04's unified reserve folds the
// operator_id into that one start's Meta), so the reconciler covers both paths uniformly
// with no approve-path special-casing.
//
// A structural list failure WARNs and retries next tick; a per-item failure WARNs and
// continues (recovered next tick). Immediately before appending, it RE-CHECKS GetEnd for
// the same key and SKIPS if a real end has landed since the list snapshot — closing the
// list→append window (ON CONFLICT DO NOTHING remains the final backstop).
func (r *Reconciler) reconcileOrphans(ctx context.Context) {
	cutoff := time.Now().Add(-effectiveGrace(r.maxToolExecWindow))
	orphans, err := r.store.ListInFlightBefore(ctx, cutoff)
	if err != nil {
		slog.Warn("reconcile: list in-flight reservations failed (retry next tick)", "err", err)
		return
	}
	for _, orphan := range orphans {
		// Pre-append GetEnd re-check: a real end may have landed between the list
		// snapshot and now (a legitimately slow tool that finished, or a concurrent
		// writer). If so, the real end won — leave the orphan alone.
		end, gerr := r.store.GetEnd(ctx, orphan.ConversationID, orphan.RequestID, orphan.ToolCallID)
		if gerr != nil {
			slog.Warn("reconcile: pre-append GetEnd re-check failed (retry next tick)",
				"conv", orphan.ConversationID, "request", orphan.RequestID,
				"tool_call", orphan.ToolCallID, "err", gerr)
			continue
		}
		if end != nil {
			continue // a real end landed since the snapshot — the real outcome wins
		}
		if err := r.store.Insert(ctx, syntheticEnd(orphan)); err != nil {
			// ON CONFLICT DO NOTHING makes a rows==0 duplicate benign; a real INSERT
			// error is a transient DB fault recovered on the next tick.
			slog.Warn("reconcile: append indeterminate end failed (retry next tick)",
				"conv", orphan.ConversationID, "request", orphan.RequestID,
				"tool_call", orphan.ToolCallID, "err", err)
			continue
		}
		slog.Info("reconcile: appended indeterminate end for crash-orphaned reservation",
			"conv", orphan.ConversationID, "tool", orphan.ToolName, "tool_call", orphan.ToolCallID)
	}
}

// syntheticEnd builds the terminal `end` fact for a crash-orphaned start. It carries the
// orphan's {conv, request, tool_call} key so the append lands on the SAME unique slot the
// real end would have (first-writer-wins via ON CONFLICT DO NOTHING). status stays
// 'error' — the migration 0011 CHECK is status IN ('ok','error'), so "indeterminate"
// rides meta, NOT a new status value (Pitfall 4). The tool is NEVER re-invoked (Pitfall 5):
// a mutating orphan's side effect may already have fired, so the conservative mark is the
// only safe close — this is why the trustworthy Mutating flag (D-02, plan 35-01) is a hard
// prerequisite of D-01d.
func syntheticEnd(orphan toolinvocations.Event) toolinvocations.Event {
	now := time.Now().UTC()
	return toolinvocations.Event{
		ConversationID: orphan.ConversationID,
		RequestID:      orphan.RequestID,
		ToolCallID:     orphan.ToolCallID,
		ToolName:       orphan.ToolName,
		Event:          toolinvocations.EventEnd,
		Seq:            orphan.Seq + 1,
		StartedAt:      orphan.StartedAt,
		EndedAt:        now,
		DurationMS:     now.Sub(orphan.StartedAt).Milliseconds(),
		Status:         "error",
		Error:          "reservation reconciled: crash-orphaned in-flight tool call (indeterminate outcome, never re-invoked)",
		Meta:           map[string]any{"indeterminate": true, "reconciled": true},
	}
}
