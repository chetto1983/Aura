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

	// (3) A non-strict profile ALSO gets a real router. This replaces the inverse assertion —
	// buildSandboxRouter used to return nil for dev/local_trusted, which is exactly the
	// host-fallback door the one-path collapse closed. A nil here would mean every tool call
	// under the default profile runs outside the box.
	nonStrict := &config.Config{
		Profile: config.ProfileDev,
		Sandbox: config.SandboxConfig{Image: "aura-sandbox:latest", EgressImage: "aura-egress:latest"},
	}
	if r := buildSandboxRouter(nonStrict); r == nil {
		t.Fatal("buildSandboxRouter under a non-strict profile must still return a router — nil permits host execution")
	}
	// And a nil config denies rather than panicking or handing back nil.
	nilCfgRouter := buildSandboxRouter(nil)
	if nilCfgRouter == nil {
		t.Fatal("buildSandboxRouter(nil) must return a denying router, not nil")
	}
	if _, err := nilCfgRouter.Route(context.Background()); err == nil {
		t.Fatal("a config-less router must DENY, not resolve a box")
	}
}

// TestBuildSandboxRouterStrictClientErrorFailsClosed proves the production composition root
// cannot turn a Docker-client configuration failure into host execution. The router must remain
// present and Route must return an error, which tells every tool to deny.
func TestBuildSandboxRouterStrictClientErrorFailsClosed(t *testing.T) {
	t.Setenv("DOCKER_HOST", "invalid-docker-host")
	cfg := &config.Config{
		Profile: config.ProfileServerProduction,
		Sandbox: config.SandboxConfig{Image: "aura-sandbox:latest"},
	}

	router := buildSandboxRouter(cfg)
	if router == nil {
		t.Fatal("Docker client construction failure returned a nil router; that permits host fallback")
	}
	handle, err := router.Route(context.Background())
	if err == nil {
		t.Fatalf("Route = (%+v, nil), want a containment error", handle)
	}
}
