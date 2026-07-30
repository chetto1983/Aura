package swarm

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

// RunnerAdapter is the concrete swarmRunner the swarm_spawn tool delegates to
// (cycle-free seam). It imports internal/agent + internal/config — fine, because
// swarm_spawn.go in the tools package imports NEITHER (the interface lives there).
// The composition root (cmd/aura) constructs one with the static Cfg and injects it
// as the tool's Runner; the per-invocation parent deps (budget/registry/client/
// llmCfg/convID) travel on the ctx via agent.WithSwarmContext, set by the agent's
// runTool. Run reads them off the ctx, builds the swarm RunConfig, and calls the
// 09-02 engine, whose runChild derives each worker registry via
// Without(parentRegistry, "swarm_spawn") (D-08/D-10 flat — no nested swarm).
type RunnerAdapter struct {
	Cfg   config.Config
	Depth int
}

// NewRunnerAdapter builds an adapter over the static config. Depth defaults to 1
// (a parent-initiated spawn); a caller may raise it for forward-compat nesting.
func NewRunnerAdapter(cfg config.Config) *RunnerAdapter {
	return &RunnerAdapter{Cfg: cfg, Depth: 1}
}

// Run resolves the parent deps off the ctx (agent.WithSwarmContext, injected by the
// agent's runTool), builds the RunConfig, and invokes the engine. A missing swarm
// context is a model-readable inline error (the tool was dispatched outside the
// agent loop — a wiring bug, surfaced without panicking). The engine's marshal
// error is the only real Go error; domain rejections ride in the JSON string. The
// returned report JSON is wrapped via tools.NewResult so a large report spills to a
// sidecar (D-15 — the ONLY spillover path).
func (a *RunnerAdapter) Run(ctx context.Context, goals []string) (tools.ToolResult, error) {
	sc, ok := agent.SwarmContext(ctx)
	if !ok {
		return tools.ToolResult{}, errors.New(
			"swarm context unavailable: swarm_spawn must be dispatched by the agent loop")
	}

	rc := RunConfig{
		ParentBudget:     sc.Budget,
		ParentRegistry:   sc.Registry, // runChild derives Without(parent, "swarm_spawn") per worker (D-08/D-10)
		Client:           sc.Client,
		LLM:              sc.LLMCfg,
		Cfg:              a.Cfg,
		ConvID:           sc.ConvID,
		Depth:            a.Depth,
		Gateway:          sc.Gateway, // relay the parent's PEP to each worker (Open Q1 full enforcement)
		ReasoningControl: sc.ReasoningControl,
	}

	out, err := Run(ctx, rc, goals)
	if err != nil {
		return tools.ToolResult{}, err
	}
	res, err := tools.NewResult(ctx, out)
	if err != nil {
		return tools.ToolResult{}, err
	}
	res.Provenance = &tools.ToolResultProvenance{Source: "swarm", Trust: tools.TrustUntrusted}
	return res, nil
}
