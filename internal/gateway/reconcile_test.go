package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestEffectiveGraceExceedsRunLifetime is the WARNING-4 collision-impossibility invariant:
// for EVERY resolved run lifetime — the two defaults (node timeout 0, wallclock 300s) AND
// operator-raised values — the effective grace strictly exceeds the lifetime, so a
// start∧¬end older than the grace can never be a still-running (or legitimately slow) tool
// whose real `end` could race the synthetic end into ON CONFLICT DO NOTHING.
func TestEffectiveGraceExceedsRunLifetime(t *testing.T) {
	for _, w := range []time.Duration{
		0,                 // AURA_LOOP_NODE_TIMEOUT_SEC default (disabled)
		300 * time.Second, // AURA_LOOP_MAX_WALLCLOCK_SEC default
		30 * time.Minute,  // lifetime == the floor: margin must still lift the grace above it
		60 * time.Minute,  // an operator raised the wallclock past the floor
		120 * time.Minute, // and further still
	} {
		if got := effectiveGrace(w); got <= w {
			t.Errorf("effectiveGrace(%s) = %s, want strictly greater than the run lifetime %s "+
				"(a within-lifetime reservation must never be reconciled as an orphan)", w, got, w)
		}
	}

	// The 30-min floor dominates for BOTH default bounds — the common production config.
	if got := effectiveGrace(0); got != reservationOrphanGrace {
		t.Errorf("effectiveGrace(0) = %s, want the %s floor", got, reservationOrphanGrace)
	}
	if got := effectiveGrace(300 * time.Second); got != reservationOrphanGrace {
		t.Errorf("effectiveGrace(300s) = %s, want the %s floor", got, reservationOrphanGrace)
	}
	// A raised lifetime auto-expands the grace to lifetime + margin (above the floor).
	if got, want := effectiveGrace(60*time.Minute), 60*time.Minute+reservationOrphanMargin; got != want {
		t.Errorf("effectiveGrace(60m) = %s, want %s (lifetime + margin)", got, want)
	}
}

// fakeReconcileStore is an in-memory inFlightReconcileStore that lets a test drive the
// list→re-check→append sequence deterministically (the list→append window a live DB race
// cannot reproduce on demand).
type fakeReconcileStore struct {
	mu        sync.Mutex
	inflight  []toolinvocations.Event
	end       *toolinvocations.Event // what GetEnd returns (the pre-append re-check result)
	getEndErr error
	listErr   error
	inserted  []toolinvocations.Event
}

func (f *fakeReconcileStore) ListInFlightBefore(_ context.Context, _ time.Time) ([]toolinvocations.Event, error) {
	return f.inflight, f.listErr
}

func (f *fakeReconcileStore) GetEnd(_ context.Context, _, _, _ string) (*toolinvocations.Event, error) {
	return f.end, f.getEndErr
}

func (f *fakeReconcileStore) Insert(_ context.Context, e toolinvocations.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, e)
	return nil
}

func (f *fakeReconcileStore) inserts() []toolinvocations.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]toolinvocations.Event(nil), f.inserted...)
}

func agedOrphan() toolinvocations.Event {
	return toolinvocations.Event{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		RequestID:      "22222222-2222-2222-2222-222222222222",
		ToolCallID:     "call-orphan-1",
		ToolName:       "skill",
		Event:          toolinvocations.EventStart,
		Seq:            1,
		StartedAt:      time.Now().Add(-time.Hour),
	}
}

// TestReconcileRechecksBeforeAppendSkips proves the pre-append GetEnd re-check closes the
// list→append window: if a real end has landed since the list snapshot, the reconciler
// appends NOTHING (the real outcome wins; ON CONFLICT DO NOTHING is only the final backstop).
func TestReconcileRechecksBeforeAppendSkips(t *testing.T) {
	realEnd := &toolinvocations.Event{Event: toolinvocations.EventEnd, Status: "ok"}
	f := &fakeReconcileStore{inflight: []toolinvocations.Event{agedOrphan()}, end: realEnd}
	r := NewReconciler(f, time.Hour, 0)

	r.reconcileOrphans(context.Background())

	if got := f.inserts(); len(got) != 0 {
		t.Fatalf("reconciler appended %d end(s) despite a real end already present; want 0 (pre-append re-check must skip)", len(got))
	}
}

// TestReconcileAppendsIndeterminateEnd proves the append shape when no real end exists: a
// single end fact keyed on the SAME triple, status='error' (never 'indeterminate' — the
// migration 0011 CHECK forbids it), with meta {indeterminate,reconciled}. The reconciler
// has NO tool-execution seam by construction, so a mutating orphan is never re-invoked —
// the appended fact is an indeterminate error, not a fresh 'ok' result.
func TestReconcileAppendsIndeterminateEnd(t *testing.T) {
	orphan := agedOrphan()
	f := &fakeReconcileStore{inflight: []toolinvocations.Event{orphan}, end: nil}
	r := NewReconciler(f, time.Hour, 0)

	r.reconcileOrphans(context.Background())

	got := f.inserts()
	if len(got) != 1 {
		t.Fatalf("appended %d end(s), want exactly 1", len(got))
	}
	e := got[0]
	if e.Event != toolinvocations.EventEnd {
		t.Errorf("event = %q, want %q", e.Event, toolinvocations.EventEnd)
	}
	if e.Status != "error" {
		t.Errorf("status = %q, want \"error\" (the CHECK is status IN ('ok','error'); indeterminate rides meta)", e.Status)
	}
	if e.Status == "indeterminate" {
		t.Errorf("status must never be 'indeterminate' (CHECK violation, Pitfall 4)")
	}
	if e.ConversationID != orphan.ConversationID || e.RequestID != orphan.RequestID || e.ToolCallID != orphan.ToolCallID {
		t.Errorf("appended end not keyed on the orphan triple: %+v", e)
	}
	if e.Meta["indeterminate"] != true || e.Meta["reconciled"] != true {
		t.Errorf("meta = %#v, want {indeterminate:true, reconciled:true}", e.Meta)
	}
}

// TestReconcileListFailureIsRecoverable proves a structural list failure WARNs and returns
// without appending (recovered next tick), never panics.
func TestReconcileListFailureIsRecoverable(t *testing.T) {
	f := &fakeReconcileStore{listErr: errors.New("db down")}
	r := NewReconciler(f, time.Hour, 0)
	r.reconcileOrphans(context.Background())
	if got := f.inserts(); len(got) != 0 {
		t.Fatalf("appended %d end(s) after a list failure, want 0", len(got))
	}
}

// TestReconcileStartStopGoleakClean proves the bounded-join lifecycle (mirrors the sweeper):
// Start launches the boot one-shot + tick loop, Stop joins it cleanly. A leaked goroutine
// fails the package via main_test.go's goleak.VerifyTestMain. A nil store / zero interval
// is a no-op, and Stop is idempotent.
func TestReconcileStartStopGoleakClean(t *testing.T) {
	f := &fakeReconcileStore{}
	r := NewReconciler(f, 10*time.Millisecond, 0)
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	r.Stop()
	r.Stop() // idempotent
	cancel()

	// A non-positive interval disables the worker: Start launches nothing, Stop still joins.
	disabled := NewReconciler(f, 0, 0)
	disabled.Start(context.Background())
	disabled.Stop()

	// A nil *Reconciler is a fully safe no-op.
	var nilR *Reconciler
	nilR.Start(context.Background())
	nilR.Stop()
}
