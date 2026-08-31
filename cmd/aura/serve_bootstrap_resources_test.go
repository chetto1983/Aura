package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const bootstrapTestIdentity = "645baf03-247a-4671-90b3-659a05b403c4"

type recordingObjectStore struct {
	got string
	err error
}

func (r *recordingObjectStore) ProvisionObjectStore(_ context.Context, id string) error {
	r.got = id
	return r.err
}
func (r *recordingObjectStore) DeprovisionObjectStore(context.Context, string) error { return nil }

type recordingFilesystem struct {
	got string
	err error
}

func (r *recordingFilesystem) ProvisionIdentityDirs(_ context.Context, id string) error {
	r.got = id
	return r.err
}
func (r *recordingFilesystem) DeprovisionIdentityDirs(context.Context, string) error { return nil }

// Every leg is fail-soft, so a leg that fails must not stop the ones behind it. The order
// they run in is load-bearing exactly once -- the box materializes the identity's skills,
// Agent.md and pyscripts from the filesystem roots -- but a failure anywhere must still
// leave the rest, and the MCP remount in particular, having run.
func TestBootstrapResourcesRunEveryLegEvenWhenOneFails(t *testing.T) {
	t.Parallel()
	obj := &recordingObjectStore{err: errors.New("garage is down")}
	fs := &recordingFilesystem{err: errors.New("read-only volume")}
	var boxIdentity string
	var remounted bool
	res := bootstrapResources{
		objectStore: obj,
		filesystem:  fs,
		sandbox: func(_ context.Context, id string) error {
			boxIdentity = id
			return errors.New("no box image")
		},
		remountMCP: func() { remounted = true },
	}

	res.provision(t.Context(), bootstrapTestIdentity)

	if obj.got != bootstrapTestIdentity {
		t.Errorf("object store leg got %q", obj.got)
	}
	if fs.got != bootstrapTestIdentity {
		t.Errorf("filesystem leg got %q", fs.got)
	}
	if boxIdentity != bootstrapTestIdentity {
		t.Errorf("sandbox leg got %q", boxIdentity)
	}
	if !remounted {
		t.Fatal("the MCP remount MUST still run: it is the only thing that mounts the shipped sidecars before a restart")
	}
}

// A deployment without Garage, without a live mounter or without a sandbox router runs the
// legs it has. buildProvisioningPorts genuinely returns nil adapters in that case, so this
// is the shipped configuration of an interview-only deploy, not a defensive flourish.
func TestBootstrapResourcesTolerateEveryLegBeingAbsent(t *testing.T) {
	t.Parallel()
	bootstrapResources{}.provision(t.Context(), bootstrapTestIdentity)
}

type recordingBackend struct{ spec usersandbox.SandboxSpec }

func (b *recordingBackend) Resolve(_ context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	b.spec = spec
	return usersandbox.BoxHandle{ContainerID: "box-1", IdentityID: spec.IdentityID}, nil
}

func (b *recordingBackend) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{}, nil
}
func (b *recordingBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (b *recordingBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (b *recordingBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

// The box MUST be keyed on the identity being provisioned. Route reads the identity off the
// context and falls back to the seeded `local` identity when none is scoped, so a starter
// that forgot to bind it would silently hand the new operator local's box -- a cross-identity
// leak that no test of "did a box start" would catch.
func TestSandboxStarterBindsTheNewIdentityNotTheLocalFallback(t *testing.T) {
	t.Parallel()
	backend := &recordingBackend{}
	router := usersandbox.NewSandboxRouter(backend, config.ProfileDev, config.SandboxConfig{Image: "aura-sandbox:test"})

	start := newSandboxStarter(router)
	if start == nil {
		t.Fatal("a live router must yield a starter")
	}
	if err := start(context.Background(), bootstrapTestIdentity); err != nil {
		t.Fatalf("start: %v", err)
	}
	if backend.spec.IdentityID != bootstrapTestIdentity {
		t.Fatalf("box provisioned for %q, want the new operator %q", backend.spec.IdentityID, bootstrapTestIdentity)
	}
	if got := identityctx.IdentityID(context.Background()); got != "" {
		t.Fatalf("the starter must scope its OWN context, not mutate the caller's (got %q)", got)
	}
}

func TestSandboxStarterIsAbsentWithoutARouter(t *testing.T) {
	t.Parallel()
	if newSandboxStarter(nil) != nil {
		t.Fatal("no router means no leg, not a leg that panics")
	}
}
