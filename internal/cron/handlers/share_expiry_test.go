package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeShareExpirer is a deterministic ShareExpirer for the handler unit tests: it records the
// scan time it was called with and returns a fixed expired count (or an error).
type fakeShareExpirer struct {
	expired int
	err     error
	gotNow  time.Time
	called  bool
}

func (f *fakeShareExpirer) ExpireDue(_ context.Context, now time.Time) (int, error) {
	f.called = true
	f.gotNow = now
	return f.expired, f.err
}

// TestShareExpiryMeta asserts the handler's static contract: the right kind, a 5-minute
// budget, no reschedule-on-recovery (the expiry sweep is idempotent).
func TestShareExpiryMeta(t *testing.T) {
	m := NewShareExpiryHandler(nil).Meta()
	if m.Kind != KindShareExpirySweep {
		t.Fatalf("kind = %q, want %q", m.Kind, KindShareExpirySweep)
	}
	if m.MaxDuration != shareExpiryMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, shareExpiryMaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("share expiry must NOT reschedule on recovery (it is idempotent)")
	}
}

// TestShareExpiryRunExpires asserts the handler drives ExpireDue with the sweep time and
// reports the expired count.
func TestShareExpiryRunExpires(t *testing.T) {
	e := &fakeShareExpirer{expired: 3}
	h := NewShareExpiryHandler(e)

	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !e.called {
		t.Fatal("handler did not call the expirer")
	}
	if e.gotNow.IsZero() {
		t.Fatal("handler did not pass a scan time")
	}
	if !strings.Contains(summary, "expired 3") {
		t.Fatalf("summary = %q, want expired 3", summary)
	}
}

// TestShareExpiryDisabled asserts a nil expirer is a no-op success (harmlessly disabled), not
// a panic.
func TestShareExpiryDisabled(t *testing.T) {
	summary, err := NewShareExpiryHandler(nil).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("disabled Run: %v", err)
	}
	if !strings.Contains(summary, "disabled") {
		t.Fatalf("disabled summary = %q, want a disabled notice", summary)
	}
}

// TestShareExpiryRunError asserts an expiry error is terminal (the dispatcher records +
// notifies it).
func TestShareExpiryRunError(t *testing.T) {
	e := &fakeShareExpirer{err: errors.New("boom")}
	if _, err := NewShareExpiryHandler(e).Run(context.Background(), Job{}); err == nil {
		t.Fatal("want terminal error from a failing expire")
	}
}
