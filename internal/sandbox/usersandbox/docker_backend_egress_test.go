package usersandbox

import (
	"context"
	"testing"
)

// docker_backend_egress_test.go is the daemon-free unit tier for the DockerBackend egress
// wiring: the construction options + getter, the deterministic name derivation, and — the
// coverage-relevant part — every egress lifecycle hook's DISABLED no-op path. With no egress
// image wired (the default, dev/local posture) findEgress short-circuits to "" and each
// launch/suspend/resume/teardown returns nil WITHOUT dialing the daemon, so a nil moby client
// is safe. The enabled paths (get-or-create, netns share) are the docker_integration tier's job.

// TestDockerBackend_EgressOptionAndGetter covers WithEgress / the default (no option) and the
// EgressImage getter that the composition root's wiring test asserts against.
func TestDockerBackend_EgressOptionAndGetter(t *testing.T) {
	t.Parallel()

	// Default: no WithEgress => sidecar disabled (empty image).
	if got := NewDockerBackend(nil, "box:img", Resources{}).EgressImage(); got != "" {
		t.Fatalf("default EgressImage() = %q, want empty (sidecar disabled)", got)
	}

	// WithEgress wires the ref through to the getter.
	b := NewDockerBackend(nil, "box:img", Resources{},
		WithEgress("aura-egress:latest"),
		WithMaterializeSources(func(string) []MaterializeSource { return nil }))
	if got := b.EgressImage(); got != "aura-egress:latest" {
		t.Fatalf("EgressImage() = %q, want aura-egress:latest", got)
	}
}

// TestDockerBackend_Names covers the deterministic container / sidecar name derivation both
// tools and the reaper key on.
func TestDockerBackend_Names(t *testing.T) {
	t.Parallel()
	const id = "u-42"
	if got := boxName(id); got != boxNamePrefix+id {
		t.Fatalf("boxName(%q) = %q, want %q", id, got, boxNamePrefix+id)
	}
	if got := egressName(id); got != egressNamePrefix+id {
		t.Fatalf("egressName(%q) = %q, want %q", id, got, egressNamePrefix+id)
	}
}

// TestDockerBackend_EgressDisabledLifecycleNoops proves every egress lifecycle hook is a safe
// no-op when the sidecar is disabled (no image wired): none dials the daemon, so all succeed
// against a nil moby client. This is the dev/local_trusted + 37-04-lifecycle posture.
func TestDockerBackend_EgressDisabledLifecycleNoops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := NewDockerBackend(nil, "box:img", Resources{}) // no WithEgress => egress disabled
	const id = "u-noop"

	if got, err := b.findEgress(ctx, id); err != nil || got != "" {
		t.Fatalf("findEgress (disabled) = (%q, %v), want (\"\", nil)", got, err)
	}
	if err := b.launchEgress(ctx, SandboxSpec{IdentityID: id}, BoxHandle{ContainerID: "box", IdentityID: id}); err != nil {
		t.Fatalf("launchEgress (disabled): %v", err)
	}
	if err := b.suspendEgress(ctx, id); err != nil {
		t.Fatalf("suspendEgress (disabled): %v", err)
	}
	if err := b.resumeEgress(ctx, id); err != nil {
		t.Fatalf("resumeEgress (disabled): %v", err)
	}
	if err := b.teardownEgress(ctx, id); err != nil {
		t.Fatalf("teardownEgress (disabled): %v", err)
	}
}
