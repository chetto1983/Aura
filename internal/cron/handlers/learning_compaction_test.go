package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/learningretention"
)

func TestLearningCompactionHandlerNonOverlapAndSummary(t *testing.T) {
	fake := &blockingLearningCompactor{entered: make(chan struct{}), release: make(chan struct{})}
	handler := NewLearningCompactionHandler(fake)
	if meta := handler.Meta(); meta.Kind != KindLearningCompaction || meta.MaxDuration != learningCompactionMaxDuration || meta.ReschedulesOnRecovery {
		t.Fatalf("Meta() = %+v", meta)
	}
	var firstSummary string
	var firstErr error
	done := make(chan struct{})
	go func() { firstSummary, firstErr = handler.Run(context.Background(), Job{}); close(done) }()
	<-fake.entered
	secondSummary, secondErr := handler.Run(context.Background(), Job{})
	if secondErr != nil || !strings.Contains(secondSummary, "already running") {
		t.Fatalf("overlap = %q, %v", secondSummary, secondErr)
	}
	close(fake.release)
	<-done
	if firstErr != nil || !strings.Contains(firstSummary, "expired 2") || !strings.Contains(firstSummary, "evicted 3") || fake.calls != 1 {
		t.Fatalf("first = %q, %v; calls=%d", firstSummary, firstErr, fake.calls)
	}
}

func TestLearningCompactionHandlerErrorAndDisabled(t *testing.T) {
	if summary, err := NewLearningCompactionHandler(nil).Run(context.Background(), Job{}); err != nil || !strings.Contains(summary, "disabled") {
		t.Fatalf("disabled = %q, %v", summary, err)
	}
	want := errors.New("neo4j down")
	h := NewLearningCompactionHandler(compactorFunc(func(context.Context) (learningretention.Report, error) { return learningretention.Report{}, want }))
	if _, err := h.Run(context.Background(), Job{}); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

type blockingLearningCompactor struct {
	mu               sync.Mutex
	calls            int
	entered, release chan struct{}
}

func (f *blockingLearningCompactor) CompactBatch(context.Context) (learningretention.Report, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	close(f.entered)
	<-f.release
	return learningretention.Report{Expired: 2, Evicted: 3}, nil
}

type compactorFunc func(context.Context) (learningretention.Report, error)

func (f compactorFunc) CompactBatch(ctx context.Context) (learningretention.Report, error) {
	return f(ctx)
}
