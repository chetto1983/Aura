package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/llm"
)

type llmRuntimeSnapshotContextKey struct{}

func withLLMRuntimeSnapshot(ctx context.Context, snapshot llm.RuntimeSnapshot) context.Context {
	return context.WithValue(ctx, llmRuntimeSnapshotContextKey{}, snapshot)
}

func (r *Runner) llmSnapshot(ctx context.Context) llm.RuntimeSnapshot {
	if snapshot, ok := ctx.Value(llmRuntimeSnapshotContextKey{}).(llm.RuntimeSnapshot); ok {
		return snapshot
	}
	if r == nil || r.runtime == nil {
		return llm.RuntimeSnapshot{}
	}
	return r.runtime.Snapshot()
}

func (r *Runner) trackerLLMSnapshot(tr *turnTracker) llm.RuntimeSnapshot {
	if tr != nil && tr.llmRuntime.Client != nil {
		return tr.llmRuntime
	}
	if r == nil || r.runtime == nil {
		return llm.RuntimeSnapshot{}
	}
	return r.runtime.Snapshot()
}
