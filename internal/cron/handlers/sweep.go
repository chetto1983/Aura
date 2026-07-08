package handlers

import (
	"context"
	"fmt"
	"time"
)

// sweep.go is the single implementation behind every system-seeded counting-sweep handler
// (identity_purge D-27, sandbox_reap D-08): each scans a due set, acts on every member, and
// reports a count. Rather than two parallel handler types (identical control flow, differing
// only in Kind/seam/messages), there is ONE countingSweepHandler parameterized by those bits and
// built by the per-sweep New*Handler constructors — so the shared shape lives in exactly one
// place (CLAUDE.md — extract a helper, never copy a parallel handler).

// sweepFn is the seam every counting sweep drives: act on the due set at the sweep clock and
// return the count acted upon. Both SandboxReaper.SuspendIdle and IdentityPurger.PurgeExpired
// have this exact signature, so one handler body serves both.
type sweepFn func(ctx context.Context, now time.Time) (int, error)

// countingSweepHandler is the shared Handler behind the counting sweeps. It is never constructed
// directly — the per-sweep files' New*Handler helpers fill it in with their Kind, seam, and
// messages. A nil seam is the disabled no-op (the sweep is off, not errored).
type countingSweepHandler struct {
	kind        TaskKind
	maxDuration time.Duration
	seam        sweepFn
	disabledMsg string
	errPrefix   string
	okFmt       string // MUST carry exactly one %d for the count.
	now         func() time.Time
}

// newCountingSweep builds the shared sweep shell. It derives the disabled no-op when seam is nil,
// so a nil-backed handler (no reaper / no purger) is a harmless success rather than a panic.
func newCountingSweep(kind TaskKind, maxDuration time.Duration, seam sweepFn, disabledMsg, errPrefix, okFmt string) countingSweepHandler {
	return countingSweepHandler{
		kind:        kind,
		maxDuration: maxDuration,
		seam:        seam,
		disabledMsg: disabledMsg,
		errPrefix:   errPrefix,
		okFmt:       okFmt,
	}
}

// Meta is the shared static contract of the counting sweeps: a per-sweep Kind + MaxDuration,
// never rescheduled on recovery (each sweep is idempotent — the next tick re-evaluates the same
// due set, so a missed run needs no catch-up).
func (h countingSweepHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: h.kind, MaxDuration: h.maxDuration, ReschedulesOnRecovery: false}
}

// Run is the shared Run body of the counting sweeps. A nil seam is a harmless no-op success
// returning disabledMsg; otherwise it invokes the seam at the resolved clock and formats okFmt
// with the count, wrapping any seam error under errPrefix.
func (h countingSweepHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.seam == nil {
		return h.disabledMsg, nil
	}
	n, err := h.seam(ctx, sweepClock(h.now))
	if err != nil {
		return "", fmt.Errorf("%s: %w", h.errPrefix, err)
	}
	return fmt.Sprintf(h.okFmt, n), nil
}

// sweepClock resolves a sweep's clock: the injected now (tests) or time.Now().UTC() in production.
func sweepClock(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now().UTC()
}
