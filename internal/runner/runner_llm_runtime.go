// runner_llm_runtime.go is the per-turn LLM snapshot seam. A ctx snapshot — set via
// withLLMRuntimeSnapshot — is now the PER-IDENTITY resolution (CRED-01/CRED-07,
// runner_identity_llm.go's IdentityLLMResolver): a caller that wants an identity's own
// OpenRouter credential resolves it there and wraps the context with
// ScopeContextToIdentitySnapshot BEFORE calling into the turn. Absent a ctx snapshot,
// llmSnapshot falls back to the runtime's own process-wide snapshot (the deployment
// client) — unchanged behavior below this seam; the resolver was added ABOVE it,
// never woven into this file's two call sites.
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
