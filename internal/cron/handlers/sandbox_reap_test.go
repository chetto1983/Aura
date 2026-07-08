package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeReaper is a deterministic SandboxReaper for the handler unit tests: it records the
// scan time it was called with and returns a fixed suspended count (or an error).
type fakeReaper struct {
	suspended int
	err       error
	gotNow    time.Time
	called    bool
}

func (f *fakeReaper) SuspendIdle(_ context.Context, now time.Time) (int, error) {
	f.called = true
	f.gotNow = now
	return f.suspended, f.err
}

// TestSandboxReapMeta asserts the handler's static contract: the right kind, a 5-minute
// budget, no reschedule-on-recovery (the reap sweep is idempotent).
func TestSandboxReapMeta(t *testing.T) {
	m := NewSandboxReapHandler(nil).Meta()
	if m.Kind != KindSandboxReap {
		t.Fatalf("kind = %q, want %q", m.Kind, KindSandboxReap)
	}
	if m.MaxDuration != sandboxReapMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, sandboxReapMaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("sandbox reap must NOT reschedule on recovery (it is idempotent)")
	}
}

// TestSandboxReapRunSuspends asserts the handler drives SuspendIdle with the sweep time and
// reports the suspended count.
func TestSandboxReapRunSuspends(t *testing.T) {
	r := &fakeReaper{suspended: 3}
	h := NewSandboxReapHandler(r)

	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.called {
		t.Fatal("handler did not call the reaper")
	}
	if r.gotNow.IsZero() {
		t.Fatal("handler did not pass a scan time")
	}
	if !strings.Contains(summary, "suspended 3") {
		t.Fatalf("summary = %q, want suspended 3", summary)
	}
}

// TestSandboxReapDisabled asserts a nil reaper is a no-op success (harmlessly disabled).
func TestSandboxReapDisabled(t *testing.T) {
	summary, err := NewSandboxReapHandler(nil).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("disabled Run: %v", err)
	}
	if !strings.Contains(summary, "disabled") {
		t.Fatalf("disabled summary = %q, want a disabled notice", summary)
	}
}

// TestSandboxReapRunError asserts a suspend error is terminal (the dispatcher records +
// notifies it).
func TestSandboxReapRunError(t *testing.T) {
	r := &fakeReaper{err: errors.New("boom")}
	if _, err := NewSandboxReapHandler(r).Run(context.Background(), Job{}); err == nil {
		t.Fatal("want terminal error from a failing suspend")
	}
}
