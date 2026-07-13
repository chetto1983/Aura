package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

type effectiveReaderFixture struct {
	snapshots []config.EffectiveCompactionSnapshot
	calls     int
}

func (f *effectiveReaderFixture) Read(context.Context) (config.EffectiveCompactionSnapshot, error) {
	i := f.calls
	if i >= len(f.snapshots) {
		i = len(f.snapshots) - 1
	}
	f.calls++
	return f.snapshots[i], nil
}

func TestCoordinatorHonorsRollbackFinalizeFence(t *testing.T) {
	b := &fakeCompactBackend{}
	r := &effectiveReaderFixture{snapshots: []config.EffectiveCompactionSnapshot{{ScopeID: "s", Version: 2, Config: config.CompactionConfig{Mode: config.CompactionEnabled, Percent: 100, RecoveryDrillPassed: true}}, {ScopeID: "s", Version: 3, Config: config.CompactionConfig{Mode: config.CompactionDisabled}}}}
	got, err := NewPersistedCompactCoordinator(b, r).Preview(t.Context(), CompactRequest{OperationID: "op", ConversationID: "c", TenantID: "t", Trigger: CompactTriggerManual})
	if err != nil || got.Status != CompactStatusDisabled || got.Activated {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

type fakeCompactBackend struct {
	preview  CompactPreview
	restored string
	err      error
}

func TestOverflowOneAttemptOneReconstruction(t *testing.T) {
	attempts, rebuilds := 0, 0
	exec := OverflowExecutor{
		Timeout: time.Second,
		Compact: func(context.Context) error { attempts++; return nil },
		Reconstruct: func(context.Context) ([]llm.Message, error) {
			rebuilds++
			return []llm.Message{{Content: "compact"}}, nil
		},
	}
	got, err := exec.Run(context.Background(), false, []llm.Message{{Content: "original"}})
	if err != nil || attempts != 1 || rebuilds != 1 || len(got) != 1 || got[0].Content != "compact" {
		t.Fatalf("got=%v err=%v attempts=%d rebuilds=%d", got, err, attempts, rebuilds)
	}
}

func TestOverflowNeverActivatesMidStream(t *testing.T) {
	attempts := 0
	exec := OverflowExecutor{Compact: func(context.Context) error { attempts++; return nil }}
	got, err := exec.Run(context.Background(), true, []llm.Message{{Content: "original"}})
	if !errors.Is(err, ErrOverflowMidStream) || attempts != 0 || got[0].Content != "original" {
		t.Fatalf("got=%v err=%v attempts=%d", got, err, attempts)
	}
}

func TestOverflowFailureReturnsOriginal(t *testing.T) {
	wantErr := errors.New("provider failed")
	exec := OverflowExecutor{Compact: func(context.Context) error { return wantErr }}
	got, err := exec.Run(context.Background(), false, []llm.Message{{Content: "original"}})
	if !errors.Is(err, wantErr) || got[0].Content != "original" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func (f *fakeCompactBackend) PreviewCompaction(context.Context, CompactRequest) (CompactPreview, error) {
	return f.preview, f.err
}
func (f *fakeCompactBackend) RestoreCompaction(_ context.Context, req CompactRequest) error {
	f.restored = req.CheckpointID
	return f.err
}

func TestCompactCoordinatorPreviewAndRestoreStayShadowOnly(t *testing.T) {
	b := &fakeCompactBackend{preview: CompactPreview{CheckpointID: "cp-1", PriorCheckpointID: "cp-0", Summary: "safe"}}
	c := NewCompactCoordinator(b, false)
	p, err := c.Preview(context.Background(), CompactRequest{OperationID: "op-1", Trigger: CompactTriggerManual})
	if err != nil || p.Status != CompactStatusDisabled || p.Activated {
		t.Fatalf("preview = %#v, %v", p, err)
	}
	r, err := c.Restore(context.Background(), CompactRequest{OperationID: "op-2", Trigger: CompactTriggerManual, CheckpointID: "cp-0", SafePoint: true})
	if err != nil || r.Status != CompactStatusRecovered || r.Activated || b.restored != "cp-0" {
		t.Fatalf("restore = %#v, %v", r, err)
	}
}

func TestCompactCoordinatorRejectsMidStreamAndMapsOutcomes(t *testing.T) {
	c := NewCompactCoordinator(&fakeCompactBackend{}, false)
	if got, _ := c.Restore(context.Background(), CompactRequest{OperationID: "op", Trigger: CompactTriggerManual}); got.Status != CompactStatusRejected {
		t.Fatalf("status = %s", got.Status)
	}
	b := &fakeCompactBackend{err: context.DeadlineExceeded}
	c = NewCompactCoordinator(b, false)
	if got, _ := c.Preview(context.Background(), CompactRequest{OperationID: "op", Trigger: CompactTriggerOverflow}); got.Status != CompactStatusTimeout {
		t.Fatalf("status = %s", got.Status)
	}
	b.err = errors.New("backend secret dsn")
	if got, _ := c.Preview(context.Background(), CompactRequest{OperationID: "op", Trigger: CompactTriggerIdle}); got.Status != CompactStatusUnavailable {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestCompactCoordinatorAcceptsEveryStableTrigger(t *testing.T) {
	for _, trigger := range []CompactTrigger{CompactTriggerManual, CompactTriggerProactive, CompactTriggerBoundary, CompactTriggerIdle, CompactTriggerModelSafePoint, CompactTriggerOverflow} {
		if err := trigger.Validate(); err != nil {
			t.Fatalf("%s: %v", trigger, err)
		}
	}
}
