package steer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// queue_sweep_unit_test.go is the daemon-free half of the sweep's coverage (no build
// tag, no Postgres): expiryReasonFor is covered in pg_store_unit_test.go alongside the
// other kind-derivation logic; this file covers expiryTraceFor (the D-08 readable
// trace text) and ExpireDue's own not-configured guard.

func TestExpiryTraceForNamesKindAndSource(t *testing.T) {
	t.Run("delegation_result names the worker's report", func(t *testing.T) {
		row := sqlc.AuraSteerQueue{Kind: string(KindDelegationResult), Source: SourceWorker}
		got := expiryTraceFor(row)
		if !strings.Contains(got, "worker") {
			t.Errorf("trace = %q, want it to name a worker's report", got)
		}
		if !strings.Contains(got, SourceWorker) {
			t.Errorf("trace = %q, want it to contain the source %q", got, SourceWorker)
		}
	})

	t.Run("steer names an operator redirect", func(t *testing.T) {
		row := sqlc.AuraSteerQueue{Kind: string(KindSteer), Source: "cockpit"}
		got := expiryTraceFor(row)
		if !strings.Contains(got, "operator") {
			t.Errorf("trace = %q, want it to name an operator steer", got)
		}
		if !strings.Contains(got, "cockpit") {
			t.Errorf("trace = %q, want it to contain the source %q", got, "cockpit")
		}
	})
}

func TestExpireDueNotConfiguredGuard(t *testing.T) {
	var s *Sweeper
	if _, err := s.ExpireDue(context.Background(), time.Now(), 10); err == nil {
		t.Fatal("ExpireDue on a nil *Sweeper = nil error, want a non-nil configuration error")
	}
	if _, err := (&Sweeper{}).ExpireDue(context.Background(), time.Now(), 10); err == nil {
		t.Fatal("ExpireDue with no pool/conv wired = nil error, want a non-nil configuration error")
	}
}

// TestMarkAndTraceRollsBackOnTraceFailure is the daemon-free proof of D-08's own
// invariant, injected through markAndTrace's two-func seam rather than a real Postgres
// transaction: a fake mark reporting success (n=1) followed by a fake trace-append that
// fails must surface that failure verbatim, never swallow it as success. expireOne's
// caller (db.WithIdentityTx) rolls back its whole transaction on ANY non-nil error from
// its closure — a contract exercised for real by internal/db's own WithTx rollback
// tests and by this package's db_integration TestExpireDue family — so a non-nil error
// here is exactly what makes "expired_at remains NULL for that row" true when this runs
// for real: the mark is never committed without its trace.
func TestMarkAndTraceRollsBackOnTraceFailure(t *testing.T) {
	traceErr := errors.New("trace append boom")
	markCalled, traceCalled := false, false
	err := markAndTrace(
		func() (int64, error) {
			markCalled = true
			return 1, nil
		},
		func() error {
			traceCalled = true
			return traceErr
		},
	)
	if !errors.Is(err, traceErr) {
		t.Fatalf("markAndTrace = %v, want the trace error surfaced verbatim (never swallowed)", err)
	}
	if !markCalled || !traceCalled {
		t.Fatalf("markCalled=%v traceCalled=%v, want both true (trace runs only after a successful mark)", markCalled, traceCalled)
	}
}

// TestMarkAndTraceSkipsTraceWhenAlreadyExpired proves the idempotency short-circuit: a
// mark reporting n=0 (a prior or concurrent pass already expired this row) must return
// errRowAlreadyExpired WITHOUT ever invoking appendTrace — an idempotent second sweep
// pass must never write a second trace turn.
func TestMarkAndTraceSkipsTraceWhenAlreadyExpired(t *testing.T) {
	traceCalled := false
	err := markAndTrace(
		func() (int64, error) { return 0, nil },
		func() error {
			traceCalled = true
			return nil
		},
	)
	if !errors.Is(err, errRowAlreadyExpired) {
		t.Fatalf("markAndTrace = %v, want errRowAlreadyExpired", err)
	}
	if traceCalled {
		t.Fatal("appendTrace was called for an already-expired row (n=0) — would write a duplicate trace")
	}
}

// TestMarkAndTraceSurfacesMarkFailure proves a mark-step failure (e.g. a transient DB
// error) is wrapped and returned without ever reaching appendTrace.
func TestMarkAndTraceSurfacesMarkFailure(t *testing.T) {
	markErr := errors.New("mark boom")
	traceCalled := false
	err := markAndTrace(
		func() (int64, error) { return 0, markErr },
		func() error {
			traceCalled = true
			return nil
		},
	)
	if !errors.Is(err, markErr) {
		t.Fatalf("markAndTrace = %v, want the mark error wrapped and surfaced", err)
	}
	if traceCalled {
		t.Fatal("appendTrace was called despite the mark step failing")
	}
}

// TestExpireOneRejectsRowWithoutIdentity covers the guard that runs BEFORE any pool
// access: a queue row whose identity_id did not scan is refused by name rather than
// carried into WithIdentityTx, where an empty tenant would widen the transaction's
// scope instead of failing it.
func TestExpireOneRejectsRowWithoutIdentity(t *testing.T) {
	s := &Sweeper{} // no pool, no conv: the guard must fire before either is touched
	err := s.expireOne(context.Background(), sqlc.AuraSteerQueue{})
	if err == nil {
		t.Fatal("expireOne(row without identity_id) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "identity_id") {
		t.Fatalf("expireOne error = %v, want it to name identity_id", err)
	}
}

// TestAllocateSweepTurnSeqRejectsMalformedConversationID pins that the conversation id
// is parsed before the two queries run, so a malformed id is a named error here rather
// than an opaque cast error from the server. q is nil precisely to prove no query runs.
func TestAllocateSweepTurnSeqRejectsMalformedConversationID(t *testing.T) {
	seq, err := allocateSweepTurnSeq(context.Background(), nil, "not-a-uuid")
	if err == nil {
		t.Fatal("allocateSweepTurnSeq(malformed) = nil error, want a parse error")
	}
	if seq != 0 {
		t.Fatalf("allocateSweepTurnSeq(malformed) seq = %d, want 0", seq)
	}
	if !strings.Contains(err.Error(), "invalid conversation_id") {
		t.Fatalf("error = %v, want it to name the invalid conversation_id", err)
	}
}
