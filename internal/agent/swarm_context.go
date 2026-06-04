package agent

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
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
type SwarmContextValue struct {
	Budget   *Budget
	Registry *tools.Registry
	Client   llm.Client
	LLMCfg   llm.Config
	ConvID   string
}

// WithSwarmContext returns a ctx carrying the parent deps the swarm_spawn runner
// adapter reads. The agent's runTool calls this before dispatching each tool so a
// swarm_spawn call can resolve the live parent budget/registry/client/config. It
// mirrors tools.WithToolCallContext.
func WithSwarmContext(ctx context.Context, budget *Budget, registry *tools.Registry, client llm.Client, llmCfg llm.Config, convID string) context.Context {
	return context.WithValue(ctx, swarmCtxKey{}, SwarmContextValue{
		Budget:   budget,
		Registry: registry,
		Client:   client,
		LLMCfg:   llmCfg,
		ConvID:   convID,
	})
}

// SwarmContext reads the parent deps the runner adapter needs off the ctx. ok is
// false when the key is absent (a non-swarm dispatch path), letting the adapter
// return a model-readable error instead of panicking.
func SwarmContext(ctx context.Context) (SwarmContextValue, bool) {
	v, ok := ctx.Value(swarmCtxKey{}).(SwarmContextValue)
	return v, ok
}
