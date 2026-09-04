package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/readiness"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// sandboxReadinessProbe reports whether the per-identity sandbox runtime is reachable. It exists
// because the one-path collapse changed what a dead Docker daemon MEANS: the box is the only
// filesystem and the only shell the agent has, so an unreachable daemon is not a degraded
// feature, it is a deployment where every single tool call answers sandbox_unavailable. Without
// this probe /readyz would answer 200 the whole time and an operator would see a healthy service
// alongside an agent insisting it cannot do anything.
//
// It follows memoryReadinessProbe's shape deliberately — same class of dependency, same treatment.
//
// The probe RESOLVED a synthetic identity's box until 2026-08-03, on the argument that only a
// real Resolve proves the image pull, the egress sidecar and the cgroup caps. The argument was
// right and the price was wrong: /readyz is this container's compose healthcheck and caddy,
// prometheus and tempo all gate on `aura: service_healthy`, so the probe ran a full Resolve —
// four volume ensures, a container resume, the whole skills tar
// materialization and an egress launch — every thirty seconds against a two-second budget. Worse,
// Resolve bumps lastUsed, so the phantom box could never be idle-reaped: one permanently running
// container plus sidecar plus volume, per deployment, forever. CheckRuntime answers the same
// operational question for the price of an ImageInspect.
func sandboxReadinessProbe(chat *chatEnv) (agui.ReadinessProbe, bool) {
	if chat == nil || chat.sandboxRouter == nil {
		return agui.ReadinessProbe{}, false
	}
	router := chat.sandboxRouter
	return agui.ReadinessProbe{
		Name: "sandbox",
		Code: readiness.CodeSandboxUnavailable,
		Check: func(ctx context.Context) error {
			return checkSandboxReadiness(ctx, router)
		},
	}, true
}

// checkSandboxReadiness asks the runtime whether it COULD produce a box, without producing one.
// What it deliberately does not catch — a cgroup cap or egress-sidecar failure that only appears
// at container creation — surfaces on the first real tool call as a fail-closed deny, which is a
// diagnosable error rather than a silent degradation. That is a fair trade for not pinning a
// container alive on a healthcheck interval.
func checkSandboxReadiness(ctx context.Context, router *usersandbox.SandboxRouter) error {
	if router == nil {
		return errors.New("sandbox router is not configured")
	}
	if err := router.CheckRuntime(ctx); err != nil {
		return fmt.Errorf("sandbox runtime: %w", err)
	}
	return nil
}
