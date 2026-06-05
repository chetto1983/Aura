package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeSweeper is a deterministic SnippetSweeper for the handler unit tests: it records
// the ttl it was called with and returns a fixed archived/kept split (or an error).
type fakeSweeper struct {
	archived []string
	kept     []string
	err      error
	gotTTL   time.Duration
	gotActor string
	called   bool
}

func (f *fakeSweeper) SweepExpiredSnippets(_ context.Context, ttl time.Duration, _ time.Time, actorID string) ([]string, []string, error) {
	f.called = true
	f.gotTTL = ttl
	f.gotActor = actorID
	return f.archived, f.kept, f.err
}

// TestSkillTTLSweepMeta asserts the handler's static contract (RESEARCH Pattern 4):
// the right kind, a 2-minute budget, no reschedule-on-recovery.
func TestSkillTTLSweepMeta(t *testing.T) {
	m := SkillTTLSweepHandler{}.Meta()
	if m.Kind != KindSkillTTLSweep {
		t.Fatalf("kind = %q, want %q", m.Kind, KindSkillTTLSweep)
	}
	if m.MaxDuration != skillTTLMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, skillTTLMaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("skill TTL sweep must NOT reschedule on recovery")
	}
}

// TestSkillTTLSweepRunArchivesAndKeeps asserts the handler drives the sweeper with the
// configured TTL + the `auto` actor (D-29) and reports the archived/kept counts.
func TestSkillTTLSweepRunArchivesAndKeeps(t *testing.T) {
	sw := &fakeSweeper{archived: []string{"old1", "old2"}, kept: []string{"fresh1"}}
	h := SkillTTLSweepHandler{Sweeper: sw, TTL: 90 * 24 * time.Hour}

	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sw.called {
		t.Fatal("handler did not call the sweeper")
	}
	if sw.gotTTL != 90*24*time.Hour {
		t.Fatalf("sweeper ttl = %s, want 90d", sw.gotTTL)
	}
	if sw.gotActor != "auto" {
		t.Fatalf("sweeper actor = %q, want auto (D-29 system row)", sw.gotActor)
	}
	if !strings.Contains(summary, "archived 2") || !strings.Contains(summary, "kept 1") {
		t.Fatalf("summary = %q, want archived 2 / kept 1", summary)
	}
}

// TestSkillTTLSweepDisabled asserts a nil sweeper or non-positive TTL is a no-op
// success (the sweep is harmlessly disabled, not an error).
func TestSkillTTLSweepDisabled(t *testing.T) {
	for _, h := range []SkillTTLSweepHandler{
		{Sweeper: nil, TTL: 90 * 24 * time.Hour},
		{Sweeper: &fakeSweeper{}, TTL: 0},
	} {
		summary, err := h.Run(context.Background(), Job{})
		if err != nil {
			t.Fatalf("disabled Run: %v", err)
		}
		if !strings.Contains(summary, "disabled") {
			t.Fatalf("disabled summary = %q, want a disabled notice", summary)
		}
	}
}

// TestSkillTTLSweepRunError asserts a sweeper error is terminal (the dispatcher records
// + notifies it, D-21).
func TestSkillTTLSweepRunError(t *testing.T) {
	sw := &fakeSweeper{err: errors.New("boom")}
	h := SkillTTLSweepHandler{Sweeper: sw, TTL: time.Hour}
	if _, err := h.Run(context.Background(), Job{}); err == nil {
		t.Fatal("want terminal error from a failing sweep")
	}
}
