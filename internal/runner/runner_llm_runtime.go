// runner_llm_runtime.go is the per-turn LLM snapshot seam. turnLocked resolves ONE
// snapshot for the whole turn through turnLLMSnapshot and seeds it onto ctx, so every
// reader below — buildAgent, the title worker, the tracker — inherits the same decision
// from llmSnapshot rather than each resolving its own.
//
// turnLLMSnapshot is where an identity's own OpenRouter credential enters the
// interactive path (CRED-05/CRED-07, D-11). llmSnapshot below it still falls back to
// the process-wide deployment snapshot, and that fallback is correct only for a
// deployment with no resolver at all (the CLI REPL, unit tests). Once a resolver is
// injected, reaching the deployment client for an identity's turn is the fail-open path
// D-11 forbids, so turnLLMSnapshot refuses instead.
package runner

import (
	"context"
	"errors"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// identityLLMSnapshotter is the per-identity credential seam the interactive turn
// resolves through. *IdentityLLMResolver satisfies it; the interface exists so a test
// can inject a refusal without standing up the encrypted key store.
type identityLLMSnapshotter interface {
	SnapshotFor(ctx context.Context, identityID string) (llm.RuntimeSnapshot, error)
}

// ErrTurnIdentityUnknown is the fail-closed refusal for a turn that reached the LLM seam
// with a per-identity resolver injected but no identity on ctx. Serving the deployment
// client there would bill the operator for a turn nobody can be charged for, which is
// precisely the substitution CRED-07 forbids.
var ErrTurnIdentityUnknown = errors.New("runner: turn has no identity to resolve an LLM credential for")

// turnLLMSnapshot resolves the snapshot one turn runs on. A snapshot already on ctx wins:
// a caller that scoped it deliberately (ScopeContextToIdentitySnapshot, or a nested
// resume) has already made the decision. Otherwise the turn's own identity decides, and
// only a Runner with no resolver at all falls through to the process-wide snapshot.
func (r *Runner) turnLLMSnapshot(ctx context.Context) (llm.RuntimeSnapshot, error) {
	if snapshot, ok := ctx.Value(llmRuntimeSnapshotContextKey{}).(llm.RuntimeSnapshot); ok {
		return snapshot, nil
	}
	if r == nil || r.identityLLM == nil {
		return r.llmSnapshot(ctx), nil
	}
	identityID := strings.TrimSpace(identityctx.IdentityID(ctx))
	if identityID == "" {
		return llm.RuntimeSnapshot{}, ErrTurnIdentityUnknown
	}
	return r.identityLLM.SnapshotFor(ctx, identityID)
}

// SetIdentityLLM makes every later turn resolve its credential from the identity that owns it.
// The serve composition root calls it once, before the listener opens; `aura chat` never does,
// so the REPL keeps the process-wide client (runner_deps.go). A nil resolver is stored as a nil
// interface, never a non-nil interface around a nil pointer.
func (r *Runner) SetIdentityLLM(resolver *IdentityLLMResolver) {
	if resolver == nil {
		r.identityLLM = nil
		return
	}
	r.identityLLM = resolver
}

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
