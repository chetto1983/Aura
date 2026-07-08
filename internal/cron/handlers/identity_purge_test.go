package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakePurger is a deterministic IdentityPurger for the handler unit tests: it records the
// scan time it was called with and returns a fixed purged count (or an error).
type fakePurger struct {
	purged int
	err    error
	gotNow time.Time
	called bool
}

func (f *fakePurger) PurgeExpired(_ context.Context, now time.Time) (int, error) {
	f.called = true
	f.gotNow = now
	return f.purged, f.err
}

// TestIdentityPurgeMeta asserts the handler's static contract: the right kind, a 5-minute
// budget, no reschedule-on-recovery (the purge saga is idempotent + resumable).
func TestIdentityPurgeMeta(t *testing.T) {
	m := NewIdentityPurgeHandler(nil).Meta()
	if m.Kind != KindIdentityPurge {
		t.Fatalf("kind = %q, want %q", m.Kind, KindIdentityPurge)
	}
	if m.MaxDuration != identityPurgeMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, identityPurgeMaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("identity purge must NOT reschedule on recovery (it is idempotent + resumable)")
	}
}

// TestIdentityPurgeRunPurges asserts the handler drives PurgeExpired with the sweep time and
// reports the purged count.
func TestIdentityPurgeRunPurges(t *testing.T) {
	p := &fakePurger{purged: 3}
	h := NewIdentityPurgeHandler(p)

	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !p.called {
		t.Fatal("handler did not call the purger")
	}
	if p.gotNow.IsZero() {
		t.Fatal("handler did not pass a scan time")
	}
	if !strings.Contains(summary, "purged 3") {
		t.Fatalf("summary = %q, want purged 3", summary)
	}
}

// TestIdentityPurgeDisabled asserts a nil purger is a no-op success (harmlessly disabled).
func TestIdentityPurgeDisabled(t *testing.T) {
	summary, err := NewIdentityPurgeHandler(nil).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("disabled Run: %v", err)
	}
	if !strings.Contains(summary, "disabled") {
		t.Fatalf("disabled summary = %q, want a disabled notice", summary)
	}
}

// TestIdentityPurgeRunError asserts a purge error is terminal (the dispatcher records +
// notifies it).
func TestIdentityPurgeRunError(t *testing.T) {
	p := &fakePurger{err: errors.New("boom")}
	if _, err := NewIdentityPurgeHandler(p).Run(context.Background(), Job{}); err == nil {
		t.Fatal("want terminal error from a failing purge")
	}
}
