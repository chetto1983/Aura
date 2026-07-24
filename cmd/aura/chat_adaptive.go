package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
)

// buildAdaptiveShadowRuntime is the production composition seam. Only an
// authoritative shadow policy at boot wires observation; canary/active serving
// remains deliberately unimplemented and therefore cannot change an action.
// The per-event controller still re-reads policy state so rollback or a
// PostgreSQL partition immediately falls back to ordinary Runner persistence.
func buildAdaptiveShadowRuntime(
	ctx context.Context,
	pool *pgxpool.Pool,
	turns *conversations.Store,
	hooks *agent.HookManager,
) (*agent.HookManager, runner.AdaptiveTurnCommitter) {
	if pool == nil || turns == nil {
		return hooks, nil
	}
	policyStore := adaptive.NewPolicyStore(pool)
	policy, err := policyStore.CurrentPolicy(ctx)
	if err != nil {
		slog.Warn("aura adaptive: policy unavailable; shadow observation disabled", "err", err)
		return hooks, nil
	}
	if policy.Mode != adaptive.PolicyShadow {
		if policy.Mode == adaptive.PolicyCanary || policy.Mode == adaptive.PolicyActive {
			slog.Error("aura adaptive: serving activation is not implemented; forcing static baseline",
				"mode", policy.Mode, "epoch", policy.Epoch)
		}
		return hooks, nil
	}

	eventStore := adaptive.NewStore(pool, adaptive.StoreConfig{})
	controller := adaptive.NewPolicyController(policyStore)
	if hooks == nil {
		hooks = agent.NewHookManager()
	}
	hooks.RegisterWithPolicy(adaptive.NewGatedDecisionHook(eventStore, controller), agent.FailOpen)
	atomicCommitter := adaptive.NewTurnCommitter(pool, turns, eventStore)
	committer := adaptive.NewPolicyAwareTurnCommitter(controller, atomicCommitter, turns)
	slog.Info("aura adaptive: shadow observation wired",
		"policy_version", policy.Version, "epoch", policy.Epoch)
	return hooks, committer
}
