package usersandbox

import (
	"bytes"
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// router_tools_test.go unit-proves the tool-facing passthroughs' NON-daemon legs: the nil-router
// guard and the structural "backend does not support X" fail-CLOSED errors a core-only Backend
// (fakeBackend implements the 5-verb seam but none of the optional artifact/file/stream
// capabilities) yields. The live in-box round-trips are the docker_integration tier's job.

// coreOnlyRouter wires a router over the core-only fakeBackend under a strict profile.
func coreOnlyRouter(t *testing.T) *SandboxRouter {
	t.Helper()
	return NewSandboxRouter(&fakeBackend{t: t}, config.ProfileSingleUserHardened, unitSandboxConfig())
}

// TestRouterExec_Passthrough covers Exec: the nil-router guard and the live-backend passthrough.
func TestRouterExec_Passthrough(t *testing.T) {
	t.Parallel()
	h := BoxHandle{ContainerID: "c", IdentityID: "id"}

	if _, err := (*SandboxRouter)(nil).Exec(context.Background(), h, ExecRequest{Command: "true"}); err == nil {
		t.Fatal("Exec on a nil router: want error, got nil")
	}
	// A live backend passes straight through (fakeBackend.Exec returns a zero result, nil).
	if _, err := coreOnlyRouter(t).Exec(context.Background(), h, ExecRequest{Command: "true"}); err != nil {
		t.Fatalf("Exec passthrough: %v", err)
	}
}

// TestRouterOptionalCaps_FailClosed covers CopyArtifactOut / WriteFile / ExecStream: a core-only
// backend does not satisfy the optional capability, so each returns a fail-CLOSED error (never a
// host fallback). The nil router hits the same backendAs guard.
func TestRouterOptionalCaps_FailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := BoxHandle{ContainerID: "c", IdentityID: "id"}

	for _, r := range []*SandboxRouter{coreOnlyRouter(t), nil} {
		if _, err := r.CopyArtifactOut(ctx, h, "/workspace/out.xlsx"); err == nil {
			t.Fatal("CopyArtifactOut without the capability: want error, got nil")
		}
		if err := r.WriteFile(ctx, h, "/workspace/in.txt", []byte("x")); err == nil {
			t.Fatal("WriteFile without the capability: want error, got nil")
		}
		if _, err := r.ExecStream(ctx, h, ExecRequest{Command: "true"}, &bytes.Buffer{}); err == nil {
			t.Fatal("ExecStream without the capability: want error, got nil")
		}
	}
}
