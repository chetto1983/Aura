package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeMemoryMentionLinker is a deterministic MemoryMentionLinker for the handler unit tests.
type fakeMemoryMentionLinker struct {
	changed int
	err     error
	called  bool
}

func (f *fakeMemoryMentionLinker) LinkMentions(_ context.Context, _ time.Time) (int, error) {
	f.called = true
	return f.changed, f.err
}

// TestMemoryMentionLinkMeta asserts the static contract: the kind the 0114 CHECK admits
// and the cron store writes, a 2-minute budget, and no reschedule-on-recovery — a missed
// sweep needs no catch-up because the next tick rebuilds the same edges from the same
// corpus.
func TestMemoryMentionLinkMeta(t *testing.T) {
	m := NewMemoryMentionLinkHandler(nil).Meta()
	if m.Kind != KindMemoryMentionLink {
		t.Fatalf("kind = %q, want %q", m.Kind, KindMemoryMentionLink)
	}
	if string(m.Kind) != "memory_mention_link" {
		t.Fatalf("kind literal = %q — it must equal the scheduler_tasks.kind the 0114 CHECK admits", m.Kind)
	}
	if m.MaxDuration != memoryMentionLinkMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, memoryMentionLinkMaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("the mention link must NOT reschedule on recovery (the next tick rebuilds the same edges)")
	}
}

func TestMemoryMentionLinkRunReportsTheCount(t *testing.T) {
	linker := &fakeMemoryMentionLinker{changed: 7}
	summary, err := NewMemoryMentionLinkHandler(linker).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !linker.called {
		t.Fatal("handler did not call the linker")
	}
	if !strings.Contains(summary, "changed 7") {
		t.Fatalf("summary = %q, want the changed count", summary)
	}
}

// A deployment with no ArcadeDB has no MENTIONS edges to rebuild: the sweep is OFF,
// which is a success, not a failing task retried every two minutes.
func TestMemoryMentionLinkDisabledWithoutALinker(t *testing.T) {
	summary, err := NewMemoryMentionLinkHandler(nil).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(summary, "disabled") {
		t.Fatalf("summary = %q, want the disabled no-op", summary)
	}
}

// A sweep failure is terminal and NAMED: the dispatcher records it and notifies (D-21), so
// a memory that has silently stopped linking is visible instead of inferred.
func TestMemoryMentionLinkRunSurfacesFailure(t *testing.T) {
	_, err := NewMemoryMentionLinkHandler(
		&fakeMemoryMentionLinker{err: errors.New("scan failed")}).Run(context.Background(), Job{})
	if err == nil || !strings.Contains(err.Error(), "memory mention link") ||
		!strings.Contains(err.Error(), "scan failed") {
		t.Fatalf("error = %v, want the wrapped sweep failure", err)
	}
}
