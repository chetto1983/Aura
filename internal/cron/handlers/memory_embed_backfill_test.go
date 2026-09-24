package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeMemoryEmbedder is a deterministic MemoryEmbedder for the handler unit tests.
type fakeMemoryEmbedder struct {
	embedded int
	err      error
	called   bool
}

func (f *fakeMemoryEmbedder) EmbedMissing(_ context.Context, _ time.Time) (int, error) {
	f.called = true
	return f.embedded, f.err
}

// TestMemoryEmbedBackfillMeta asserts the static contract: the kind the 0091 CHECK admits
// and the cron store writes, a 5-minute budget, and a fire at the boot catch-up. The daemon
// kicks this pass at boot (next_run_at = now) because a route change restarts it and memory
// stays lexical until the pass runs; without the fire the catch-up advanced the kicked task
// and skipped it. Measured on the lab VM 2026-09-24: the kick at 20:49:17 was skipped and
// memory stayed lexical until the scheduled run at 20:54:27.
func TestMemoryEmbedBackfillMeta(t *testing.T) {
	m := NewMemoryEmbedBackfillHandler(nil).Meta()
	if m.Kind != KindMemoryEmbedBackfill {
		t.Fatalf("kind = %q, want %q", m.Kind, KindMemoryEmbedBackfill)
	}
	if string(m.Kind) != "memory_embed_backfill" {
		t.Fatalf("kind literal = %q — it must equal the scheduler_tasks.kind the 0091 CHECK admits", m.Kind)
	}
	if m.MaxDuration != memoryEmbedBackfillMaxDuration {
		t.Fatalf("max duration = %s, want %s", m.MaxDuration, memoryEmbedBackfillMaxDuration)
	}
	if !m.ReschedulesOnRecovery {
		t.Fatal("the backfill must fire at the boot catch-up, or the boot kick is skipped and memory stays lexical for a whole schedule")
	}
}

func TestMemoryEmbedBackfillRunReportsTheCount(t *testing.T) {
	embedder := &fakeMemoryEmbedder{embedded: 9}
	summary, err := NewMemoryEmbedBackfillHandler(embedder).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !embedder.called {
		t.Fatal("handler did not call the embedder")
	}
	if !strings.Contains(summary, "embedded 9") {
		t.Fatalf("summary = %q, want the embedded count", summary)
	}
}

// A deployment with no ArcadeDB or no embedding sidecar has no vectors to backfill: the
// sweep is OFF, which is a success, not a failing task retried every five minutes.
func TestMemoryEmbedBackfillDisabledWithoutAnEmbedder(t *testing.T) {
	summary, err := NewMemoryEmbedBackfillHandler(nil).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(summary, "disabled") {
		t.Fatalf("summary = %q, want the disabled no-op", summary)
	}
}

// A sweep failure is terminal and NAMED: the dispatcher records it and notifies (D-21), so
// a memory that has silently stopped being embedded is visible instead of inferred.
func TestMemoryEmbedBackfillRunSurfacesFailure(t *testing.T) {
	_, err := NewMemoryEmbedBackfillHandler(
		&fakeMemoryEmbedder{err: errors.New("sidecar down")}).Run(context.Background(), Job{})
	if err == nil || !strings.Contains(err.Error(), "memory embed backfill") ||
		!strings.Contains(err.Error(), "sidecar down") {
		t.Fatalf("error = %v, want the wrapped sweep failure", err)
	}
}
