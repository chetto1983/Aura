package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/llm"
)

// runner_reasoning.go threads the per-turn FIXED reasoning-effort override (the web
// Composer selector, 37E) from the HTTP run handler (plan 06) down to agent
// construction via a ctx value — mirroring WithThreadLockHeld/threadLockHeld
// (runner.go) so the override composes with the existing TurnWithModelUserMessage
// split without a new Turn-variant method. An absent key is the "auto" case: no
// override rides ctx, so buildAgent leaves LlmAgentConfig.ReasoningOverride zero and
// the agent runs today's adaptive path unchanged (D-04, zero regression).

// reasoningOverrideKey is the private ctx key for the fixed per-turn reasoning effort.
// A struct{} key (never a string) means no unrelated ctx value can be misread as an
// effort (T-37E-04-CTX: ctx-key collision mitigated by construction).
type reasoningOverrideKey struct{}

// WithReasoningOverride marks ctx with a FIXED per-turn reasoning effort selected and
// validated upstream (the server owns the symbol→effort map, plan 06). buildAgent reads
// it into LlmAgentConfig.ReasoningOverride so the agent bypasses the adaptive classifier
// and forces req.Reasoning on a reasoning target (D-04/D-08). The caller sets this ONLY
// for a fixed level; auto/absent leaves ctx untouched.
func WithReasoningOverride(ctx context.Context, effort llm.ReasoningEffort) context.Context {
	return context.WithValue(ctx, reasoningOverrideKey{}, effort)
}

// reasoningOverride returns the fixed per-turn effort carried on ctx and whether one was
// set. The zero result ("", false) is the auto case — no override, adaptive path runs.
func reasoningOverride(ctx context.Context) (llm.ReasoningEffort, bool) {
	eff, ok := ctx.Value(reasoningOverrideKey{}).(llm.ReasoningEffort)
	return eff, ok
}

// ReasoningOverride is the exported reader for the fixed per-turn effort carried on ctx — the
// public read side of WithReasoningOverride. The runtime consumer (buildAgent) uses the
// package-internal reasoningOverride; this lets the upstream writer of the seam (the agui run
// handler, plan 06) assert in a unit test that a validated fixed level was actually threaded
// (and that auto/absent threads nothing). The zero result ("", false) is the auto case.
func ReasoningOverride(ctx context.Context) (llm.ReasoningEffort, bool) {
	return reasoningOverride(ctx)
}
