package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// TestBuildSandboxRouterWiresEgress is the always-on (docker-FREE) regression guard for the
// SBX-04 composition-root wiring: it proves buildSandboxRouter's newSandboxBackend passes
// cfg.Sandbox.EgressImage into usersandbox.WithEgress, so a strict-profile box gets the always-on
// egress floor. This is the guard the SBX-04 BLOCKER lacked — the floor was built + launched by
// DockerBackend but buildSandboxRouter never called WithEgress, so launchEgress was a permanent
// no-op in the shipped binary. A nil moby client is safe here: NewDockerBackend never dials at
// construction, so the applied WithEgress option is observable via the EgressImage() accessor
// without a daemon. It mirrors serve_provisioning_test.go's docker-free assertion style.
func TestBuildSandboxRouterWiresEgress(t *testing.T) {
	// (1) The config-sourced egress image reaches WithEgress: a DISTINCT digest-pinned ref (not
	// the default const) proves newSandboxBackend passes cfg.Sandbox.EgressImage through, never a
	// hardcoded literal.
	const pinnedEgress = "registry.example.test/aura-egress@sha256:abc"
	strictCfg := &config.Config{
		Profile: config.ProfileSingleUserHardened,
		Sandbox: config.SandboxConfig{Image: "aura-sandbox:latest", EgressImage: pinnedEgress},
	}
	if got := newSandboxBackend(nil, strictCfg).EgressImage(); got != pinnedEgress {
		t.Fatalf("newSandboxBackend must wire WithEgress(cfg.Sandbox.EgressImage): EgressImage()=%q, want %q", got, pinnedEgress)
	}

	// (2) The DEFAULT egress posture is floor-ON: a default-loaded cfg carries a NON-EMPTY egress
	// image (aura-egress:latest), and newSandboxBackend wires it — so a strict box gets the tenancy
	// floor by default (SC#4/D-06), not off. config.Load fail-fasts on an empty LLM key, so set a
	// placeholder; clear AURA_SANDBOX_EGRESS_IMAGE to force the in-code default regardless of host.
	t.Setenv("OPENROUTER_API_KEY", "sk-test-egress-wiring")
	t.Setenv("AURA_SANDBOX_EGRESS_IMAGE", "")
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.Sandbox.EgressImage != "aura-egress:latest" {
		t.Fatalf("default egress image must be non-empty aura-egress:latest (floor-on, SC#4), got %q", loaded.Sandbox.EgressImage)
	}
	if got := newSandboxBackend(nil, loaded).EgressImage(); got == "" {
		t.Fatal("default-loaded cfg must yield a non-empty EgressImage() — the SBX-04 floor is on-by-default (SC#4)")
	}

	// (3) A non-strict profile stays a host-direct no-op: buildSandboxRouter returns nil and never
	// reaches the WithEgress / Docker-client path (SC-4). Mirrors TestBuildDispatchRegistersSandboxReap.
	nonStrict := &config.Config{
		Profile: config.ProfileDev,
		Sandbox: config.SandboxConfig{Image: "aura-sandbox:latest", EgressImage: "aura-egress:latest"},
	}
	if r := buildSandboxRouter(nonStrict); r != nil {
		t.Fatal("buildSandboxRouter under a non-strict profile must be nil (host-direct no-op, no WithEgress path)")
	}
}

// TestBuildSandboxRouterStrictClientErrorFailsClosed proves the production composition root
// cannot turn a strict profile into host-direct execution when the Docker client configuration is
// invalid. The strict router must remain present and Route must report routed=true plus an error,
// which tells every sandbox-aware tool to deny instead of executing on the host.
func TestBuildSandboxRouterStrictClientErrorFailsClosed(t *testing.T) {
	t.Setenv("DOCKER_HOST", "invalid-docker-host")
	cfg := &config.Config{
		Profile: config.ProfileServerProduction,
		Sandbox: config.SandboxConfig{Image: "aura-sandbox:latest"},
	}

	router := buildSandboxRouter(cfg)
	if router == nil {
		t.Fatal("strict Docker client construction failure returned a nil router; that permits host fallback")
	}
	handle, routed, err := router.Route(context.Background())
	if !routed {
		t.Fatal("strict Docker client construction failure returned routed=false; tool would execute on the host")
	}
	if err == nil {
		t.Fatalf("Route = (%+v, %v, nil), want a containment error", handle, routed)
	}
}
