package agent

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
)

// swarmCtxKey is the private unexported context key carrying the parent's
// per-invocation deps to the swarm_spawn runner adapter. It mirrors the
// tools.WithToolCallContext idiom EXACTLY: a private struct-typed key + a typed
// With/read pair, no exported key, no string key. Only the swarm_spawn dispatch
// reads it; every other tool ignores the key.
type swarmCtxKey struct{}

// SwarmContextValue carries the parent deps the internal/swarm RunnerAdapter needs
// to build a swarm RunConfig: the shared Budget tree, the parent tool Registry
// (the adapter derives the worker registry via Without(reg, "swarm_spawn")), the
// LLM Client + Config the workers run against, and the conversation id that keys
// each worker SessionID and the transcript dir. config.Config (RunDir, swarm caps)
// is NOT carried here — the adapter holds it as a construction-time field, so this
// package never imports internal/config.
//
// CONCURRENT-READ CONTRACT (AG-062): every field is shared READ-ONLY across the
// fanned-out swarm workers. The *Registry is immutable post-boot (the agent
// registry is built once and never mutated during a run), so concurrent Get/All
// reads are safe without locking. The Budget's shared *atomic.Int32 step counter
// is the ONLY mutable cross-worker state and is atomic by construction (Child
// forks a distinct dedup ring per worker — no shared map). The llm.Client must be
// safe for concurrent Stream calls (the production OpenRouter client is). No
// worker writes any of these fields; a worker that needs per-call state derives it
// (Budget.Child, Without(registry,...)) rather than mutating the shared value. The
// `-race` swarm fan-out test (runner_adapter_test / swarm_test) guards this.
type SwarmContextValue struct {
	Budget   *Budget
	Registry *tools.Registry
	Client   llm.Client
	LLMCfg   llm.Config
	ConvID   string
	// Gateway is the parent's Phase-35 policy PEP, relayed to each swarm worker so a
	// headless child dispatch is enforced too (Open Q1 full enforcement). nil is a no-op.
	Gateway *gateway.Gateway
}

// WithSwarmContext returns a ctx carrying the parent deps the swarm_spawn runner
// adapter reads. The agent's runTool calls this before dispatching each tool so a
// swarm_spawn call can resolve the live parent budget/registry/client/config. It
// mirrors tools.WithToolCallContext.
func WithSwarmContext(
	ctx context.Context,
	budget *Budget,
	registry *tools.Registry,
	client llm.Client,
	llmCfg llm.Config,
	convID string,
	gw *gateway.Gateway,
) context.Context {
	return context.WithValue(ctx, swarmCtxKey{}, SwarmContextValue{
		Budget:   budget,
		Registry: registry,
		Client:   client,
		LLMCfg:   llmCfg,
		ConvID:   convID,
		Gateway:  gw,
	})
}

// SwarmContext reads the parent deps the runner adapter needs off the ctx. ok is
// false when the key is absent (a non-swarm dispatch path), letting the adapter
// return a model-readable error instead of panicking.
func SwarmContext(ctx context.Context) (SwarmContextValue, bool) {
	v, ok := ctx.Value(swarmCtxKey{}).(SwarmContextValue)
	return v, ok
}
